# TODO — dashboard, taxonomia e site (sessão pós-#382)

Marcar `[x]` ao concluir, com red-proof (mutação → vermelho → revert) onde houver teste.

## Diagnóstico (confirmado no código)

- [x] Badge de versão: `shell.templ` `__gogogoSyncBadge` lê o **body** de `/health` ("ok") e retorna cedo; o tag vive no **header** `X-Gogogo-Build`. `session.js` lê o header certo. → badge nunca sincroniza, fica no valor SSR.
- [x] Odômetro: `data-effect="window.__gogogoOdo($el, ...)"`. No Datastar v1, `$el` é reescrito para `$['el']` (sinal "el", inexistente) → `__gogogoOdo(undefined)` retorna silencioso. SSR (valor final) é o que aparece. `$el` não é usado em nenhum outro lugar do repo.
- [x] Grid "installed": nenhuma recipe bundled registra fragmento `dashboard.card` (só out-of-tree via `Slots.Add`); todo/inbox são hardcoded na seção "Welcome back". Grid sempre vazio → msg mentirosa.
- [x] Nav: `MenuOnly` existe (Config é o precedente) mas UserMenu tem entradas hardcoded. Sidebar = Inbox + Collab(dashboard/todo/whiteboard/notes) + AI(chat/review) + Operate(room/graph) + "Extensions".
- [x] Terminologia: "Extensions" em só 2 lugares user-facing (sidebar label + msg vazia). `_extensions/` → renomeado para `_recipes/` (unificação total, ver item 5).

## Decisões (evidência, não gosto)

- **graph → Servant (KindPlugin)**: não instalável (recursivo — renderiza o próprio mapa, já estabelecido em docs/scope-taxonomy) e não desinstalável (sem receipt, manual mount) → KindFeature mente. Servant = "binary works but loses a capability" = exatamente o panorama. Nav: sidebar → menu de perfil (MenuOnly).
- **dashboard → segue KindFeature (Recipe)**: TEM receipt (`.recipes/dashboard.yml`) + self-mount (Register) → passa no critério instalável/desinstalável. Host by role, recipe by kind. Nav: sidebar → menu de perfil (superfície home, não destino diário — mesmo rationale do Config).
- **Terminologia → "Recipe" em tudo** (CLI `gogogo recipe`, skill, `.recipes/`, UI, landing, docs). `_extensions/` renomeado para `_recipes/` para o mesmo conceito valer em paths também.
- **Grid = todas as recipes instaladas** (uma por destino de sidebar + fragmentos de slot out-of-tree). Seção welcome duplicada some; odômetros moram nos cards todo/inbox. Vazio honesto.
- **/config** = inventário agrupado Core/Servants/Recipes com status.

## Fix batch A (bugs autônomos) — commits separados

- [x] Badge de versão: ler header `X-Gogogo-Build` em `__gogogoSyncBadge` + teste (red-proof no markup) → f1d8c10d
- [x] Odômetro: trocar `$el` por lookup por id (padrão document.getElementById do repo) + teste → b47a5c96

## Terminologia + grid

- [x] Sidebar label "Extensions" → "Recipes"; msg vazia honesta; chi_test atualizado
- [x] Grid lista recipes instaladas (de ui.Modules) + fragmentos; welcome section mescla para o grid → 65e1ad13

## Taxonomia e navegação

- [x] graph KindFeature → KindPlugin + taxonomy doc atualizada (seção "fits neither label")
- [x] Graph + Dashboard: MenuOnly + entradas no UserMenu (padrão hardcoded do Config, com trim comments)
- [x] Testes de navegação (visibility/nav parity, perfil leva plataforma, sidebar só receitas) → c3c31627

## Config agrupado

- [x] `/config` lista core + servants agrupados por tipo com status (+ recipes, agrupando por tipo) → c7ca1eef

## Site audit (por último, valida estado corrigido) — sem subagents, sequencial

- [x] Seção por seção: extrair claims, double-check contra código, corrigir stale no mesmo commit — landing (7 níveis, catalog regenerado, micro-fix "Core, Plugin or Feature" → "Core, Servant or Recipe"), ui-shell (MenuOnly), cli/mcp/skill (verbos unificados), extensibility (spellings de comando), taxonomy + admin-dashboard + features + architecture/overview/todo-example/semântica (sem achados), varredura de padrões (sem resíduos); check-docs/site-check/build-landing verdes
- [x] Gates: `make site-check`, `make css-check`, `make check-docs`, scoped `go test`, `make ci-local-fast`

## Ship

- [x] Commit(s) por assunto (conventional, sem atribuição de IA), push, PR template completo, CI verde, merge (PR #383)
- [ ] Avisar: `chore: release` do release-please é do humano revisar/mergear
