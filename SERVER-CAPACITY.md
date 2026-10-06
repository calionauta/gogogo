# SERVER-CAPACITY.md — runbook operacional (server.calionauta.com)

> **Escopo:** T2 (isolamento de recursos) + T3 (capacidade) para o host de deploy.
> Material **operacional e específico deste host** — não faz parte do contrato do
> template gogogo. Fica na raiz (não em `docs/`) para não virar página do site
> (`site/build.mjs` publica `docs/*.md`).
>
> **Regra mestra:** este host não é máquina de lint. Quando precisar rodar,
> rode escopado e com teto via `make lint-safe` / `scripts/lint-safe.sh`.

---

## Status (2026-10-06)

| Item | Estado | Detalhe |
|---|---|---|
| Matar o lint desgovernado | ✅ feito | 2 rodadas full-repo mortas; host voltou a responder |
| T1 — instruções do agente | ✅ feito | trabalho em `fix/lint-safe-host-aware` (pushed) + aplicado no clone do server |
| T2 — isolamento em cgroup | ✅ feito e **provado** | alocação de 900 MB em teto de 700 M → exit 137, host vivo |
| Disco 96% → 85% | ✅ feito | `go clean -cache` + versões antigas do Playwright (~8,6 GB) |
| golangci-lint corrigido | ✅ feito | 2.12.2 (quebrado) → **2.14.0** em `~/go/bin` + PATH |
| **Swap +6 GB** | ⛔ pendente | requer `sudo` com senha (não tenho) |
| **Journal vacuum** | ⛔ pendente | requer `sudo` |
| **VACUUM `bb.db`** | ⏸ adiado | exige parar o `bb-daemon` → **interrompe thread ativa** (ver §4) |

---

## 0. O que aconteceu (evidência)

4 vCPU (Neoverse-N1) / 8 GB / disco **96%** / swap 2 GB **100% usado** / load **36**.
`golangci-lint` **full-repo** (≈2,6 GB RSS) disparado pelo agente `pi`, concorrendo
com `bb-daemon` (**3,7 GB**) + produção + `opencode`. Soma > 8 GB → swap → thrash →
host para de responder.

Detalhes que importam:
- O processo que derrubou **não** usava `cache clean` — a causa foi **escopo**
  (lintou `web-packages.sh`, o repo inteiro), não runtime.
- O `golangci-lint` do PATH (`/usr/local/bin`, **2.12.2**, built go1.26) **nem
  funciona** neste repo (go1.27.1): `the Go language version used to build
  golangci-lint is lower than the targeted Go version`. Toda rodada queimava
  recursos e terminava em erro.
- O host tem `go 1.22.2` no PATH (repo pede 1.27.1) → `GOTOOLCHAIN=auto` baixa
  toolchain a cada comando; e **não há `sudo` sem senha**.

---

## 1. T1 — correção das instruções (feito)

- **Repo local (branch `fix/lint-safe-host-aware`, pushed):** novo
  `scripts/lint-safe.sh` + `make lint-safe`; `.air.toml` sem lint por save;
  `.lefthook.yml` escopado; skill sem `cache clean`; AGENTS/docs atualizados.
- **Clone do server (`~/repos/gogogo-fullstack-template`):** `scripts/lint-safe.sh`
  adicionado; `AGENTS.md` e `skills/gogogo-coding-standards/SKILL.md` corrigidos
  (adições cirúrgicas — **nenhum arquivo de trabalho do agente foi tocado**).
- **Regra global do agente** em `~/.pi/agent/AGENTS.md` (fora do worktree): nunca
  `golangci-lint run ./...`, nunca `cache clean`; usar `lint-safe`/escopado.
- **golangci-lint 2.14.0** instalado em `~/go/bin` (mesmo pin do CI) e
  `$HOME/go/bin` prependado no PATH de `~/.profile`/`~/.bashrc`/`~/.bash_profile`.
  Backup do anterior em `~/go/bin/golangci-lint.bak-2.13.2`.

> Merge pendente: abrir PR de `fix/lint-safe-host-aware` → `master`.

## 2. T2 — isolamento (feito, provado)

`scripts/lint-safe.sh` mede RAM livre + cores em tempo de execução e:
full só com `MemAvailable ≥ 4096 MB`; senão packages alterados; sempre
`GOMEMLIMIT`/`--concurrency`; sempre num **escopo cgroup** (`systemd-run`).

```bash
# prova de contenção (rodada): 900 MB num teto de 700 M → exit 137, host vivo
systemd-run --user --scope --quiet -p MemoryMax=700M -p MemorySwapMax=0 \
  bash -c 'python3 -c "x=bytearray(900*1024*1024)"'
```

> Nota: `systemd-run -p Nice=10` é **rejeitado** neste systemd
> (`Unknown assignment`) — o script usa `nice -n 15` no prefixo.

End-to-end no server: `bash scripts/lint-safe.sh` rodou full capado
(`GOMEMLIMIT=3243MiB`, conc=4) e terminou com o host saudável (~3 GB usados).

Para build/test pesado, aplique o mesmo teto:

```bash
systemd-run --user --scope --quiet \
  -p MemoryMax=2500M -p MemorySwapMax=512M -p CPUQuota=200% -p Nice=10 \
  go test -race -count=1 ./features/... ./internal/...
```

## 3. T3 — capacidade

### 3.1 Feito
```bash
go clean -cache                      # libera ~6,8 GB (regenerável)
# + remoção das versões antigas de ~/.cache/ms-playwright (mantém a última)
# resultado: disco 96% (3,4 GB livres) → 85% (11 GB livres)
```

### 3.2 Pendente — precisa de `sudo` com senha (rode você)

```bash
# swap extra de 6 GB SEM mexer no /swapfile atual (swapoff em swap cheio = OOM)
sudo fallocate -l 6G /swapfile2
sudo chmod 600 /swapfile2
sudo mkswap /swapfile2
sudo swapon /swapfile2
echo '/swapfile2 none swap sw 0 0' | sudo tee -a /etc/fstab

# journal
sudo journalctl --vacuum-size=200M

# conferir
swapon --show; free -h; df -h /
```

## 4. Adiado — VACUUM do `bb.db` (1,2 GB)

**Exige parar o `bb-daemon`** → interromperia a thread de coding ativa. Não foi
feito por causa disso. Faça numa janela sem thread:

```bash
sqlite3 ~/.bb/bb.db ".backup ~/.bb/bb.db.bak-$(date +%Y%m%d-%H%M)"
sqlite3 ~/.bb/bb.db "PRAGMA page_count; PRAGMA freelist_count; PRAGMA page_size;"
systemctl --user stop bb-daemon.service
sqlite3 ~/.bb/bb.db "PRAGMA journal_mode=WAL; VACUUM;"
sqlite3 ~/.bb/bb.db "PRAGMA wal_checkpoint(TRUNCATE);"
systemctl --user start bb-daemon.service
systemctl --user show bb-daemon.service -p MemoryCurrent -p MemoryPeak
```

Se `MemoryPeak` seguir alto, baixar `MemoryMax` do bb (hoje 4,5 G) para ~3 G —
**medindo antes**, pois teto baixo demais mata o bb dentro do cgroup.

---

## 5. Backup do trabalho do servidor

Antes de qualquer coisa, snapshot do estado não commitado:

```
~/.ops-backups/20261006-151337/
  tracked.patch        # git diff HEAD (ci.yml, .golangci.yml, Makefile)
  status.txt, HEAD.txt, branch.txt, stash-create.sha
  pi-AGENTS.md, pi-hook/, pi-skills/, bb-automations/, bb/skills*
```

Nada no worktree do agente foi sobrescrito; as mudanças aplicadas nele são
**adições** (`AGENTS.md`, skill, novo `scripts/lint-safe.sh`).

## 6. Risco residual

- O binário **`/usr/local/bin/golangci-lint` (2.12.2) continua quebrado** — não
  pude substituí-lo (sem `sudo`). Shells que não passam por `lint-safe` ainda o
  pegam; ele falha rápido no repo (go1.27.1), mas não está capado. Se puder:
  `sudo ln -sf ~/go/bin/golangci-lint /usr/local/bin/golangci-lint`.
- Enquanto prod e dev dividem 8 GB, picos continuam possíveis. Revisitar CAX31
  (16 GB) quando houver orçamento.
