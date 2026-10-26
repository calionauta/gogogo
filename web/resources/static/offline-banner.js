// offline-banner.js — transport state for the offline outbox.
//
// Owned by internal/components/offline_banner.templ (one <script src> per
// page). Extracted from the template because it is a state machine, not
// markup: banner state, the service-worker bridge, the replay kick and the
// dropped-change notices.
//
// TRUE VANILLA ISLAND (documented in internal/components): no Datastar
// primitive receives service-worker messages, so this file subscribes and
// re-enters Datastar by dispatching gogogo:queued / gogogo:synced events.
// Everything else (online/offline/visibilitychange) is owned by data-on
// attributes on the banner element.
(function () {
  var banner = document.getElementById("offline-banner");
  var text = document.getElementById("offline-banner-text");
  if (!banner || !text) return;

  var OFFLINE_SYNC = banner.dataset.offlineSync === "true";
  var userEmail = banner.dataset.userEmail || "";

  // queuedPending survives the online flap: a mutation queued while offline
  // is still unsynced until the SW says "sync-end". Without it the banner
  // flipped straight to "Online" on reconnect even when the replay had
  // failed and the mutation was still in IndexedDB (the optimistic row then
  // outlived every reload).
  var queuedPending = false;

  // needsAuth is the 401 pause: the replay reached the server and the
  // SESSION is gone. Retrying cannot fix that, so the queue stops re-arming
  // (no Background Sync loop, no page kicks) and says so until a fresh
  // sign-in / page load gets a valid session again.
  var needsAuth = false;

  // replaying mirrors the SW's pass so the page's kick loop cannot start a
  // second pass while one is already running (the SW serializes them, but
  // piling messages on a slow pass is noise, not help).
  var replaying = false;

  // currentOwner is a hash of the signed-in email (no PII at rest). The SW
  // stamps each queue entry with it and only replays entries for the owner
  // currently signed in, so a mutation queued by one person can never be
  // written into another person's account on a shared device.
  var currentOwner = "";

  var countWaiters = {};
  var countSeq = 0;
  var KICK_DELAYS = [1000, 2500, 5000];

  function setState(state) {
    var cls = "is-" + state;
    banner.classList.remove("is-online", "is-syncing", "is-queued", "is-needs-auth", "is-offline");
    banner.classList.add(cls);
    // The auth pause needs an ACTION, not just a sentence: the one thing
    // that fixes it is a sign-in, and it must return here.
    var signin = document.getElementById("offline-banner-signin");
    if (signin) {
      signin.hidden = state !== "needs-auth";
      if (state === "needs-auth") {
        signin.href = "/login?next=" + encodeURIComponent(location.pathname + location.search);
      }
    }
    text.textContent = state === "syncing"
      ? "Syncing changes\u2026"
      : state === "needs-auth"
        ? "Changes queued \u2014 sign in to sync"
        : state === "queued"
          ? "Changes queued \u2014 retrying sync\u2026"
          : state === "offline"
            ? (OFFLINE_SYNC
              ? "Offline \u2014 changes are queued and will sync when you reconnect"
              : "Offline \u2014 online-only mode; requests will fail when network is down")
            : "Online";
  }

  // toast surfaces a dropped/no-longer-replayable change. The server-side
  // toast stack (#toast-container) is reused so it looks like every other
  // notice; textContent keeps user data out of HTML.
  function toast(message, kind) {
    var host = document.getElementById("toast-container");
    if (!host) {
      console.warn("offline: " + message);
      return;
    }
    var el = document.createElement("div");
    el.className = "alert alert-" + (kind || "warning") + " shadow-lg";
    el.setAttribute("role", "status");
    el.textContent = message;
    host.appendChild(el);
    setTimeout(function () { el.remove(); }, 8000);
  }

  function plural(n, one, many) {
    return n + " " + (n === 1 ? one : many);
  }

  function showDropNotice(count, reason) {
    if (!count) return;
    var why = reason === "other-account" ? " (queued by another account)"
      : reason === "stale" ? " (older than a week)"
        : reason === "queue-full" ? " (the queue was full)"
          : "";
    toast(plural(count, "change", "changes") + " couldn't be saved" + why, "warning");
  }

  function ownerKey(email) {
    var value = String(email || "").trim().toLowerCase();
    if (!value) return Promise.resolve("");
    if (!(window.crypto && crypto.subtle) || !window.TextEncoder) {
      return Promise.resolve("email:" + value);
    }
    return crypto.subtle.digest("SHA-256", new TextEncoder().encode(value))
      .then(function (buf) {
        var out = "";
        var bytes = new Uint8Array(buf);
        for (var i = 0; i < bytes.length; i++) {
          out += bytes[i].toString(16).padStart(2, "0");
        }
        return out;
      })
      .catch(function () { return "email:" + value; });
  }

  function postOwner() {
    if (!OFFLINE_SYNC || !navigator.serviceWorker) return Promise.resolve();
    return new Promise(function (resolve) {
      navigator.serviceWorker.ready.then(function (registration) {
        if (registration.active) {
          registration.active.postMessage({ type: "owner", owner: currentOwner });
        }
        resolve();
      }).catch(function () { resolve(); });
    });
  }

  // queueCount asks the OWNER of the queue (the service worker) how many
  // mutations are waiting. The page must not open the queue database itself:
  // a second opener without the SW's upgrade handler once created a v1
  // database with no store, after which every queued mutation was lost.
  function queueCount() {
    if (!OFFLINE_SYNC || !navigator.serviceWorker) return Promise.resolve(0);
    return new Promise(function (resolve) {
      var id = ++countSeq;
      countWaiters[id] = resolve;
      navigator.serviceWorker.ready.then(function (registration) {
        if (registration.active) {
          registration.active.postMessage({ type: "queue-status", id: id });
        } else {
          delete countWaiters[id];
          resolve(0);
        }
      }).catch(function () {
        delete countWaiters[id];
        resolve(0);
      });
      // A terminated/overloaded worker must not leave the caller hanging.
      setTimeout(function () {
        if (countWaiters[id]) {
          delete countWaiters[id];
          resolve(0);
        }
      }, 2000);
    });
  }

  function askSWToDrain() {
    if (!OFFLINE_SYNC || !navigator.serviceWorker) return;
    navigator.serviceWorker.ready.then(function (registration) {
      if (registration.active) {
        // The owner rides the kick so a replay can never start against the
        // previous account while the owner write is still in flight.
        registration.active.postMessage({ type: "replay-queue", owner: currentOwner });
      }
    }).catch(function () {});
  }

  // kickSync asks the SW to drain, then VERIFIES the queue actually moved
  // and asks again with backoff while items remain. The online event fires
  // before the network is usable, so the first attempt can fail; without
  // this retry the mutation sat in IndexedDB with nothing scheduled and the
  // user had to keep toggling offline to flush it. Bounded: after the
  // budget the SW's Background Sync registration owns the retry.
  function kickSync(attempt) {
    if (!navigator.onLine || needsAuth || replaying) return;
    askSWToDrain();
    if (attempt >= KICK_DELAYS.length) return;
    setTimeout(function () {
      if (!navigator.onLine || needsAuth || replaying) return;
      queueCount().then(function (n) {
        if (n > 0) kickSync(attempt + 1);
      });
    }, KICK_DELAYS[attempt]);
  }

  // updateOnline folds the coarse connectivity flag into the queue states.
  function updateOnline() {
    setState(!navigator.onLine
      ? "offline"
      : (needsAuth ? "needs-auth" : (queuedPending ? "queued" : "online")));
    // The presence pill is fed only by server broadcasts and never learns
    // about a client-side disconnect on its own — tie it to navigator.onLine.
    reflectPresence(navigator.onLine);
  }

  // Toggle the stale/offline styling on every presence pill. We only react
  // to navigator.onLine: "syncing" means the connection is already back, so
  // the live count should stay visible.
  function reflectPresence(online) {
    var pills = document.querySelectorAll(".online-pill");
    for (var i = 0; i < pills.length; i++) {
      pills[i].classList.toggle("is-offline", !online);
    }
  }

  if (OFFLINE_SYNC && navigator.serviceWorker) {
    navigator.serviceWorker.addEventListener("message", function (e) {
      var data = e.data || {};
      if (data.type === "queue-status") {
        var waiter = countWaiters[data.id];
        if (waiter) {
          delete countWaiters[data.id];
          waiter(data.count);
        }
      } else if (data.type === "queued-item") {
        // The feature owns its optimistic DOM; the worker only reports what
        // it queued. Re-enter Datastar with the item as event detail.
        queuedPending = true;
        window.dispatchEvent(new CustomEvent("gogogo:queued", { detail: data.item }));
      } else if (data.type === "sync-start") {
        queuedPending = true;
        replaying = true;
        setState("syncing");
        reflectPresence(true);
      } else if (data.type === "sync-end") {
        queuedPending = false;
        needsAuth = false;
        replaying = false;
        setState("online");
        reflectPresence(true);
        // Notify Datastar consumers: the queue drained, so list pages refetch
        // through their resync button (see ListResync's gogogo:synced
        // listener). The live SSE broadcast is not enough on its own — it can
        // land while the tab's stream is still re-establishing after offline,
        // with nothing after it.
        window.dispatchEvent(new CustomEvent("gogogo:synced"));
      } else if (data.type === "sync-auth") {
        // Session gone mid-queue. Keep the mutations, stop the retry loop,
        // and ask for the one action that helps. Do NOT dispatch
        // gogogo:queued here: it resets the composer and would wipe text a
        // user is typing while a background replay finds an expired session.
        queuedPending = true;
        needsAuth = true;
        replaying = false;
        setState("needs-auth");
      } else if (data.type === "sync-error") {
        // Still queued. Online means the replay attempted and did not drain
        // (network not ready, 5xx); offline means the mutation was just
        // queued. Never claim "Online" while items remain.
        queuedPending = navigator.onLine;
        replaying = false;
        setState(queuedPending ? "queued" : "offline");
        reflectPresence(navigator.onLine);
        // Notify Datastar consumers: a mutation just queued offline, so
        // forms that set a pending signal before posting can reset
        // immediately. This SW postMessage is awaited before the intercepted
        // request completes.
        window.dispatchEvent(new CustomEvent("gogogo:queued"));
      } else if (data.type === "queue-lost") {
        showDropNotice(data.count, data.reason);
      }
    });
  }

  // Owned by the data-on:online__window / data-on:offline__window /
  // data-on:visibilitychange__document attributes on the banner div: the
  // functions below are state writers, Datastar owns the subscriptions. The
  // online path orchestrates the SW replay kick — a multi-step imperative
  // sequence (postMessage, wait, conditional update) with no declarative
  // shape.
  window.__gogogoBannerOnline = function () {
    // A connectivity event is also the cheapest place to retry the one thing
    // an auth pause is waiting for: a session that came back (a sign-in in
    // any tab shares the cookie). One attempt; a still-expired session
    // pauses again.
    needsAuth = false;
    queueCount().then(function (n) {
      queuedPending = n > 0;
      kickSync(0);
      setTimeout(function () {
        if (!banner.classList.contains("is-syncing")) updateOnline();
      }, 500);
    });
  };
  window.__gogogoBannerOffline = function () { updateOnline(); };
  // A hidden tab's replay can fail while it is in the background (timers
  // throttled, socket asleep). Re-check the queue when the tab is visible
  // again instead of waiting for another online flap.
  window.__gogogoBannerVisible = function () {
    if (document.visibilityState !== "visible") return;
    needsAuth = false;
    queueCount().then(function (n) {
      if (n > 0 && navigator.onLine) kickSync(0);
    });
  };

  // __gogogoLogout is the shell's sign-out action. It asks the SW for the
  // queue size first: a sign-out discards queued mutations (they must never
  // replay under the next account on a shared device), so the user confirms
  // that loss instead of discovering it after signing back in.
  window.__gogogoLogout = function () {
    function submit() {
      window.fetch("/logout", { method: "POST", credentials: "same-origin" }).then(function () {
        if (navigator.serviceWorker) {
          navigator.serviceWorker.ready.then(function (registration) {
            if (registration.active) {
              registration.active.postMessage({ type: "clear-user-data" });
            }
          }).catch(function () {});
        }
        window.location.href = "/login";
      }).catch(function () {
        window.location.href = "/login";
      });
    }
    if (!OFFLINE_SYNC || !navigator.serviceWorker) {
      submit();
      return;
    }
    queueCount().then(function (n) {
      if (n > 0 && !window.confirm(plural(n, "queued change", "queued changes") + " will be discarded. Sign out anyway?")) {
        return;
      }
      submit();
    }).catch(submit);
  };

  updateOnline();
  if (!OFFLINE_SYNC) return;

  // Bind the queue to this signed-in account before the first count/kick,
  // then resume whatever a previous visit left queued (a reload while
  // offline, or a replay that failed).
  ownerKey(userEmail).then(function (owner) {
    currentOwner = owner;
    return postOwner();
  }).then(function () {
    return queueCount();
  }).then(function (n) {
    if (n === 0) return;
    queuedPending = true;
    updateOnline();
    if (navigator.onLine) kickSync(0);
  });
})();
