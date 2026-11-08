# Todo list — unified recipes (worktree recipes-site-audit)

Sem task tool no OpenCode; este arquivo é a lista. Marcar `[x]` ao concluir, com red-proof (mutação → vermelho → revert).

- [x] Fase 0: MyGo shell + smoke + research + `Recommended` + `Deps.Queue/RealLLM/SimulatedLLM`
- [x] Fase 1: landing + config-view self-mounting (`5d6efee3`)
- [x] Fase 2a: review self-mounting (`33c7958f`)
- [x] Fase 2b: credits self-mounting (ledger + `Service.Start` + shutdown via `Deps.OnShutdown`)
- [ ] Fase 2c: room self-mounting (GoAkt host + shutdown via `Deps.OnShutdown`)
- [ ] Fase 2d: whiteboard+notes self-mounting (Persister+DocStore+federated via Deps)
- [ ] Fase 2e: chat self-mounting (bootAI: panels+actions+router+queue)
- [ ] Fase 2f: todo self-mounting (display/search/LLM/onboarding/consumer/health via `Service`)
- [ ] Fase 3: notifications self-mounting (bootInbox) + graph/license (se já forem recipe)
- [ ] Fase 4: receipts `.recipes/<id>.yml` para todos os bundled + `recipe uninstall <builtin>` funcional
- [ ] Fase 4b: scaffold escreve receipts + TUI aceita recomendado (non-interactive já feito)
- [ ] Fase 5: docs/site/skill/README/AGENTS/llms fechamento (glossário único)
- [ ] Fase 6: gates finais — `go test ./internal/server -race`, `make ci-local-fast`, `make site-check`, commit final
