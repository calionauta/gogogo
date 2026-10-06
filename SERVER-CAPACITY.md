# SERVER-CAPACITY.md — runbook operacional (server.calionauta.com)

> **Escopo:** T2 (isolamento de recursos) + T3 (capacidade) para o host de deploy.
> Material **operacional e específico deste host** — não faz parte do contrato do
> template gogogo. Fica na raiz (não em `docs/`) para não virar página do site.
>
> **Regra mestra:** este host não é máquina de lint. Quando precisar rodar,
> rode escopado e com teto via `make lint-safe` / `scripts/lint-safe.sh`.

---

## Status — resolvido (2026-10-06)

| Item | Estado | Detalhe |
|---|---|---|
| Host | ✅ | load **36 → 3,5** · mem livre ~4 GB · disco 96% → **91%** (com +4 GB de swap) |
| Lint desgovernado | ✅ | 2 rodadas full-repo mortas; lints são read-only, nada perdido |
| T1 — instruções | ✅ | branch `fix/lint-safe-host-aware` (pushed) + aplicado no clone do server + regra global em `~/.pi/agent/AGENTS.md` |
| T2 — cgroup | ✅ provado | 900 MB em teto de 700 M → exit 137, host vivo |
| Disco | ✅ | `go clean -cache` + Playwright antigo + `apt-get clean` |
| `golangci-lint` | ✅ | **2.14.0** em `/usr/local/bin` (PATH) **e** `~/go/bin` — antes: 2.12.2 quebrado |
| Go | ✅ | **1.27.1** em `/usr/local/go` (+ `/etc/profile.d/go.sh`) |
| Swap | ✅ | `/swapfile2` **+4 GB** (total 6 GB; `/swapfile` de 2 GB mantido) |
| Journal | ✅ | vacuum rodou (0 B a liberar; já estava enxuto) |
| `bb.db` VACUUM | ✅ **desnecessário** | `freelist_count = 0`, `auto_vacuum = incremental` → o 1,2 GB é dado vivo (ver §4) |

## 0. O que aconteceu

4 vCPU (Neoverse-N1) / 8 GB / disco **96%** / swap 2 GB **100% usado** / load **36**.
`golangci-lint` **full-repo** (≈2,6 GB) disparado pelo agente `pi`, concorrendo com
`bb-app` (**3,7 GB**) + produção + `opencode` → soma > 8 GB → swap → thrash →
host para de responder. O processo que derrubou **não** usava `cache clean`: a
causa era **escopo** (`web-packages.sh`, o repo inteiro). Além disso, o
`golangci-lint` do PATH (2.12.2, built go1.26) **nem rodava** neste repo
(go1.27.1): `the Go language version used to build golangci-lint is lower than
the targeted Go version`.

## 1. T1 — correção das instruções (feito)

- **Repo (branch `fix/lint-safe-host-aware`, pushed):** `scripts/lint-safe.sh` +
  `make lint-safe`; `.air.toml` sem lint por save; `.lefthook.yml` escopado;
  skill sem `cache clean`; AGENTS/docs atualizados. **PR → `master` pendente.**
- **Clone do server:** `scripts/lint-safe.sh` adicionado; `AGENTS.md` e
  `skills/gogogo-coding-standards/SKILL.md` corrigidos (adições cirúrgicas —
  nenhum arquivo de trabalho do agente foi tocado).
- **Regra global** em `~/.pi/agent/AGENTS.md`: nunca `run ./...`, nunca
  `cache clean`; usar `lint-safe`/escopado.
- **Toolchain alinhado ao CI:** `golangci-lint 2.14.0` em `/usr/local/bin` e
  `~/go/bin`; `Go 1.27.1` em `/usr/local/go`. Backup do binário antigo em
  `/usr/local/bin/golangci-lint.bak-2.12.2`.

## 2. T2 — isolamento (feito, provado)

`scripts/lint-safe.sh` mede RAM livre + cores em tempo de execução e decide:
full só com `MemAvailable ≥ 4096 MB`; senão packages alterados; sempre
`GOMEMLIMIT`/`--concurrency`; sempre num **escopo cgroup**.

```bash
# prova (rodada): 900 MB num teto de 700 M → exit 137, host vivo
systemd-run --user --scope --quiet -p MemoryMax=700M -p MemorySwapMax=0 \
  bash -c 'python3 -c "x=bytearray(900*1024*1024)"'
```

> `systemd-run -p Nice=10` é rejeitado neste systemd (`Unknown assignment`) — o
> script usa `nice -n 15` no prefixo.

## 3. T3 — capacidade (feito)

```bash
go clean -cache                       # ~6,8 GB (regenerável)
# + remoção das versões antigas de ~/.cache/ms-playwright (mantém a última)
apt-get clean
# swap +4 GB (o /swapfile de 2 GB foi mantido)
fallocate -l 4G /swapfile2 && chmod 600 /swapfile2 && mkswap /swapfile2 && swapon /swapfile2
echo '/swapfile2 none swap sw 0 0' >> /etc/fstab
# toolchain
install -m 0755 /home/deploy/go/bin/golangci-lint /usr/local/bin/golangci-lint   # 2.14.0
# Go 1.27.1 em /usr/local/go + /etc/profile.d/go.sh
```

## 4. `bb.db` — por que **não** fazer VACUUM

Medido a quente (read-only):

```
PRAGMA page_size = 4096
PRAGMA page_count = 292852        # ~1,2 GB
PRAGMA freelist_count = 0         # 0 páginas livres
PRAGMA auto_vacuum = 2            # INCREMENTAL
```

`freelist_count = 0` → **não há nada a recuperar**. O banco não está inchado; o
1,2 GB é dado vivo (3.001 threads). Um `VACUUM` pararia o `bb-app` (que hospeda
o processo `pi` ativo) **sem ganho algum** — por isso não foi feito, e não é
necessário. Se um dia o banco inchar de verdade, cheque `freelist_count` antes
de cogitar a janela de manutenção.

## 5. Backup do trabalho do servidor

Antes de qualquer coisa, snapshot do estado não commitado:

```
~/.ops-backups/20261006-151337/
  tracked.patch        # git diff HEAD (ci.yml, .golangci.yml, Makefile)
  status.txt, HEAD.txt, branch.txt, stash-create.sha
  pi-AGENTS.md, pi-hook/, pi-skills/, bb-automations/, bb/skills*
```

Nenhum arquivo do worktree do agente foi sobrescrito; no clone dele só houve
**adições** (`AGENTS.md`, skill, novo `scripts/lint-safe.sh`).

## 6. Próximos passos (não-urgentes)

- Abrir/mergear o PR de `fix/lint-safe-host-aware` → `master`.
- Revisitar CAX31 (16 GB) quando houver orçamento: enquanto prod e dev dividem
  8 GB, picos continuam possíveis — as proteções acima os contêm, não os evitam.
