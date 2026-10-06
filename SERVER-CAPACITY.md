# SERVER-CAPACITY.md — runbook operacional (server.calionauta.com)

> **Escopo:** T2 (isolamento de recursos) + T3 (capacidade). É material
> **operacional e específico deste host** — não faz parte do contrato do template
> gogogo. Fica na raiz em vez de `docs/` de propósito, para não virar página do
> site (`site/build.mjs` publica `docs/*.md`).
>
> **Regra mestra:** o servidor não é máquina de lint. Quando precisar rodar,
> rode escopado e com teto, via `make lint-safe` / `scripts/lint-safe.sh`.

---

## 0. Diagnóstico em uma linha (evidência)

4 vCPU (Neoverse-N1) / 8 GB / disco 96% / swap 2 GB **100% usado** / load **36**.
`golangci-lint` **full-repo** disparado por agente = **2,57 GB RSS**, concorrendo
com `bb-daemon` (**3,7 GB**) + produção + `opencode`/`pi`. A soma passa dos 8 GB →
swap → thrash → host para de responder. O `cache clean` agrava, mas **a rodada
que derrubou não o usava** — a causa é *escopo*, não runtime.

---

## 1. T2 — Isolamento (já embutido no `lint-safe`)

`scripts/lint-safe.sh` já envolve a rodada num **escopo cgroup transitório**:
se um lint fugir do controle, ele morre *dentro do próprio cgroup* em vez de
acordar o OOM killer global.

```bash
# o que o lint-safe executa por baixo, em hosts com systemd de usuário:
systemd-run --user --scope --quiet \
  -p MemoryMax=1500M -p MemorySwapMax=256M \
  -p CPUQuota=200% -p Nice=10 \
  golangci-lint run <pkgs> --concurrency 2 --timeout 5m
```

**Aplicar o mesmo teto a build/test** (os outros dois picos, além do lint):

```bash
# build
go build ./...                 # barato; normalmente não precisa de escopo
# testes (o pico real: -race em features/todo)
systemd-run --user --scope --quiet \
  -p MemoryMax=2500M -p MemorySwapMax=512M -p CPUQuota=200% -p Nice=10 \
  go test -race -count=1 ./features/... ./internal/...
```

**Verificação T2:** rode `make lint-safe` e observe que o processo some sozinho
se estourar o teto, sem derrubar `bb-daemon`:

```bash
# em outra sessão
watch -n2 'free -h; ps -eo rss,args --sort=-rss | grep -E "golangci|go test" | head'
```

---

## 2. T3 — Capacidade (exige janela de manutenção)

> Ordem importa: **disco → swap → banco → toolchain**. O VACUUM do SQLite precisa
> de espaço temporário (~1,2 GB) e o novo swap precisa de disco livre. Faça numa
> janela sem thread ativa codando.

### 2.1 Disco — hoje 96% (3,4 GB livres)

```bash
df -h /
sudo du -xh --max-depth=1 /home/deploy | sort -rh | head -15   # o que ocupa

# 1) limpeza sancionada (allow-list; sempre veja o dry-run antes)
~/scripts/cleanup.sh --dry-run
~/scripts/cleanup.sh

# 2) journal do sistema
sudo journalctl --vacuum-size=200M

# 3) cache de build do Go (6,8 GB) — libera agora, repovoa no próximo build
go clean -cache

# 4) docker: só o que é descartável (NUNCA prune de imagem de produção)
docker system df
docker builder prune -f
docker image prune -f          # dangling only
```

`~/backups` (5,5 GB) e `~/.cache` (12 GB) são os maiores candidatos — revise e
pode timestamps antigos manualmente antes de apagar qualquer coisa.

### 2.2 Swap — hoje 2 GB, 100% usado (RAM 8 GB)

Regra: **adicione um segundo swapfile sem mexer no atual** (dar `swapoff` num
swap cheio pode disparar OOM). Depois de liberar disco:

```bash
# criar swap extra de 6 GB
sudo fallocate -l 6G /swapfile2
sudo chmod 600 /swapfile2
sudo mkswap /swapfile2
sudo swapon /swapfile2

# persistir
echo '/swapfile2 none swap sw 0 0' | sudo tee -a /etc/fstab

# conferir
swapon --show
cat /proc/sys/vm/swappiness   # 10 está bom: mantenha baixo
```

Swap é **rede de segurança**, não solução: só engorda a margem antes do próximo
pico. A solução é não rodar o pico (T1) + isolamento (T2).

### 2.3 `bb.db` — 1,2 GB + WAL, 3.001 dirs de thread

O banco do bb-daemon é grande e ajuda a inflar o processo (que já bate 3,7 GB).

```bash
# 0) BACKUP primeiro (db pode ser copiado a quente com sqlite .backup)
sqlite3 ~/.bb/bb.db ".backup ~/.bb/bb.db.bak-$(date +%Y%m%d-%H%M)"

# 1) espaço recuperável antes de decidir
sqlite3 ~/.bb/bb.db "PRAGMA page_count; PRAGMA freelist_count; PRAGMA page_size;"
#   recuperável ≈ freelist_count × page_size

# 2) janela: parar o daemon (interrompe threads/automações)
systemctl --user stop bb-daemon.service

# 3) VACUUM (precisa de ~1,2 GB de disco livre — por isso 2.1 vem antes)
sqlite3 ~/.bb/bb.db "PRAGMA journal_mode=WAL; VACUUM;"
sqlite3 ~/.bb/bb.db "PRAGMA wal_checkpoint(TRUNCATE);"

# 4) revisar threads antigas (ARQUIVAR, não apagar às cegas)
ls -lt ~/.bb/thread-storage | tail -20

# 5) subir de volta e medir
systemctl --user start bb-daemon.service
systemctl --user show bb-daemon.service -p MemoryCurrent -p MemoryPeak
```

Se depois do VACUUM o `MemoryPeak` continuar alto, considere baixar o
`MemoryMax` do `bb-daemon` de 4,5 G para ~3 G (arquivo em
`~/.config/systemd/user/bb-daemon.service.d/`). **Não baixe sem medir** — um teto
baixo demais mata o bb dentro do cgroup.

### 2.4 Toolchain — alinhar com o CI

Hoje o host tem `go 1.22.2` no PATH (o repo pede **go 1.27.1**) e
`golangci-lint 2.12.2` (built go1.26), enquanto o CI usa **2.14.0**. O
`GOTOOLCHAIN=auto` baixa o toolchain a cada comando — disco e CPU à toa.

```bash
# Go 1.27.1 (linux/arm64)
curl -fsSL https://go.dev/dl/go1.27.1.linux-arm64.tar.gz -o /tmp/go.tgz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz
export PATH=/usr/local/go/bin:$PATH
go version            # → go1.27.1

# golangci-lint 2.14.0 (release oficial; o bottle do Homebrew recusa go1.27)
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
  | sh -s -- -b ~/.local/bin v2.14.0
golangci-lint version # → 2.14.0
```

Ganho: menos downloads de toolchain e lint alinhado ao CI (menos risco de
erro de type-check espúrio por versão).

### 2.5 Verificação final

```bash
free -h; swapon --show; uptime; df -h /
journalctl --since "1 hour ago" | grep -iE "oom|killed process" || echo "sem OOM"
cd ~/repos/gogogo-fullstack-template && make lint-safe
```

---

## 3. Hardening opcional — forçar a política mesmo se a LLM ignorar

Um agente pode chamar `golangci-lint run ./...` direto, sem saber do
`lint-safe`. Para tornar a política inevitável, um *shim* no PATH:

```bash
# ~/.local/bin/golangci-lint  (precisa vir ANTES de /usr/local/bin no PATH)
#!/usr/bin/env bash
case "${1:-}" in
  version|cache) exec /usr/local/bin/golangci-lint "$@" ;;   # não mexer nesses
  run)           exec bash "$(git rev-parse --show-toplevel)/scripts/lint-safe.sh" "${@:2}" ;;
  *)             exec /usr/local/bin/golangci-lint "$@" ;;
esac
```

Trade-off: centraliza a política, mas exige `chmod +x` e garantir a ordem do
`PATH`; e qualquer repo fora do gogogo cai no `lint-safe` dele. Ative só depois
de validar. Alternativa mais simples: manter a regra no `AGENTS.md`/skill (feito)
e usar o `lint-safe` como o único comando documentado.

---

## 4. Resumo executável

| Camada | Ação | Quando |
|---|---|---|
| T0 | `kill <pid golangci-lint>` se em curso | agora |
| T1 | `make lint-safe`; nunca `cache clean`; nunca `run ./...` | sempre |
| T2 | escopo `systemd-run -p MemoryMax=…` (já no `lint-safe`) | sempre |
| T3.1 | disco: `cleanup.sh`, `journalctl --vacuum`, `go clean -cache` | janela |
| T3.2 | swap +6 GB (`/swapfile2`) | janela |
| T3.3 | `bb.db` VACUUM + checkpoint + poda de threads | janela |
| T3.4 | Go 1.27.1 + golangci-lint 2.14.0 | janela |
| — | CAX31 (16 GB) | **fora de escopo (sem orçamento) — revisitar depois** |
