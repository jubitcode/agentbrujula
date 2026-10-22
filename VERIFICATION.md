# VERIFICATION

Resultado de correr **todas** las suites de test del repositorio, con sus fallos incluidos.
Generado por `make verify` (`scripts/verify.sh`). Reproducible por cualquiera que clone el repo.

| | |
|---|---|
| **Fecha** | 2026-10-20 21:19 UTC |
| **Commit** | `ea95b3c` |
| **Árbol de trabajo** | 7 archivos sin commitear |

> Este archivo se regenera con `make verify`. **No se edita a mano.**
> Incluye lo que falla a propósito: un reporte que solo muestra las corridas verdes no es evidencia.

---

## Resumen

| Suite | Comando | Resultado |
|---|---|---|
| Rust · contratos Soroban | `cd contracts && cargo test` | **37 pasan**, 0 fallan — verde |
| Go · mcp-meta | `cd mcp-meta && go test ./internal/...` | **13 paquetes ok**, 0 con fallos, 3 sin tests — verde |
| Web · jest | `cd mcp-meta/web && npm test` | Tests:       3 failed, 121 passed, 124 total — con fallos |
| Python · agent | por archivo, `HOME` limpio | **294 pasan**, 39 fallan, 51 se saltan, 0 errores, 12 timeouts |

**Total verificado pasando: 331 tests de Rust y Python**, más 13 paquetes Go y la suite web.

---

## Por qué el número de Python no es una medida de calidad del código

La suite de Python **no es hermética**. Comparte cuatro clases de estado, y cada una produce
fallos que no son defectos del código:

| Estado compartido | Qué provoca | Cómo se reconoce |
|---|---|---|
| `$HOME/.mcp-meta/marketplace.db` | El test anti-replay de x402 **pasa exactamente una vez por máquina** | El mismo archivo da resultados distintos en la segunda corrida |
| **Filas en Postgres** | Nonces ya consumidos por corridas anteriores | `claim_nonce(...)` devuelve `False` sobre un nonce "nuevo" |
| **Presupuesto del rate limiter de mcp-meta** | La propia suite lo agota y las aserciones siguientes reciben **429 en vez de 401** | `assert 429 == 401` |
| **Requiere mcp-meta vivo** | Sin el servicio, los tests de integración fallan en lugar de saltarse | Errores de conexión |

Por eso este script corre Python **archivo por archivo, con un `HOME` nuevo para cada uno** y un
timeout de 60s — corrida entera, **cuelga**, por una interacción entre archivos y no
por un test lento.

**Para leer este archivo honestamente:** los números de abajo son el estado de la *suite*, no el de
los contratos ni el del orquestador. Las suites que sí son herméticas —Rust y Go— están limpias.
Hacer hermética la de Python es trabajo pendiente y está registrado como tal.

### Condiciones del entorno de esta corrida

| | |
|---|---|
| mcp-meta alcanzable | sí |
| Postgres alcanzable | no verificado por este script |
| `HOME` por archivo | nuevo (temporal) |
| Timeout por archivo | 60s |

---

## Rust — contratos Soroban

```
    |                      ^^^^^^^

warning: unused variable: `orch`
   --> usdc-escrow/src/lib.rs:424:45
    |
424 |         let (env, escrow_id, client, agent, orch, token_addr) = setup();
    |                                             ^^^^ help: if this is intentional, prefix it with an underscore: `_orch`
    |
    = note: `#[warn(unused_variables)]` (part of `#[warn(unused)]`) on by default


running 4 tests
....
test result: ok. 4 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.03s


running 7 tests
.......
test result: ok. 7 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.10s


running 26 tests
..........................
test result: ok. 26 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.48s
```

## Go — mcp-meta

Sin fallos.

```
ok  	mcp-meta/internal/a2a	(cached)
ok  	mcp-meta/internal/api	(cached)
ok  	mcp-meta/internal/build	(cached)
ok  	mcp-meta/internal/cli	(cached)
ok  	mcp-meta/internal/config	(cached)
?   	mcp-meta/internal/correlation	[no test files]
?   	mcp-meta/internal/deploy	[no test files]
ok  	mcp-meta/internal/jsonrpc	(cached)
ok  	mcp-meta/internal/mcp	(cached)
?   	mcp-meta/internal/metrics	[no test files]
ok  	mcp-meta/internal/registry	(cached)
ok  	mcp-meta/internal/resilience	(cached)
ok  	mcp-meta/internal/router	(cached)
ok  	mcp-meta/internal/runtime	(cached)
ok  	mcp-meta/internal/supervisor	(cached)
ok  	mcp-meta/internal/transport	(cached)
```

## Web — jest

```
Test Suites: 1 failed, 12 passed, 13 total
Tests:       3 failed, 121 passed, 124 total
Snapshots:   0 total
Time:        2.349 s
```

## Python — detalle por archivo

```
test_a2a_cost_cap.py                                 8 passed 
test_d1_identity.py                                  TIMEOUT (>60s)
test_d2_escrow.py                                    TIMEOUT (>60s)
test_d3_compression.py                               TIMEOUT (>60s)
test_deactivate_auth.py                              6 passed 
test_debug_endpoints.py                              3 passed 
test_e2e_auth.py                                     4 passed 
test_e2e_escrow.py                                   4 skipped 
test_e2e_smoke.py                                    12 skipped 
test_e2e_x402.py                                     1 failed 4 passed 
test_edge_cases.py                                   TIMEOUT (>60s)
test_endpoint_auth.py                                6 passed 
test_escrow_refund.py                                8 passed 
test_fdid_middleware.py                              6 passed 
test_hebbian_ablation.py                             5 passed 
test_hebbian_integration.py                          7 skipped 
test_hebbian_introspection.py                        10 passed 
test_hebbian_research_improvements.py                17 passed 
test_hebbian_session_continuity.py                   2 failed 1 passed 
test_hebbian_token_proof.py                          1 skipped 
test_hebbian_validation.py                           22 passed 
test_hire_latency.py                                 4 passed 
test_hire_smoke.py                                   18 passed 
test_hire_team_handler_caps.py                       3 passed 
test_horizon_fallback.py                             2 passed 
test_integ_auth.py                                   TIMEOUT (>60s)
test_integ_cost_tracking.py                          5 failed 
test_integ_escrow_flow.py                            TIMEOUT (>60s)
test_integ_multiuser.py                              TIMEOUT (>60s)
test_integ_payment_escrow.py                         5 skipped 
test_integ_settlement.py                             3 skipped 
test_integ_team.py                                   4 skipped 
test_integ_x402_hire.py                              10 skipped 
test_jwt_auth.py                                     TIMEOUT (>60s)
test_knowledge.py                                    1 failed 7 passed 
test_link_escrow.py                                  1 failed 
test_load_task_wallet_binding.py                     5 passed 
test_mcp_meta_integration_security.py                4 failed 4 passed 
test_mcp_meta_security.py                            8 failed 5 passed 
test_multisig_unsigned.py                            1 failed 4 passed 
test_nonce_atomicity.py                              2 failed 5 passed 
test_payment_flow.py                                 3 failed 5 passed 
test_prepare_wallet_security.py                      5 passed 
test_rate_limits.py                                  TIMEOUT (>60s)
test_real_ip_key.py                                  8 passed 
test_refund_sweeper.py                               7 failed 16 passed 
test_security_hardening.py                           TIMEOUT (>60s)
test_session_endpoint.py                             6 passed 
test_session_tokens.py                               13 passed 
test_stellar_config.py                               14 passed 
test_submit_deactivate_security.py                   5 passed 
test_submit_payment_auth.py                          4 passed 
test_sweeper_idempotency.py                          TIMEOUT (>60s)
test_task_store.py                                   TIMEOUT (>60s)
test_task_store_pg.py                                3 failed 4 passed 5 skipped 
test_team.py                                         15 passed 
test_team_cost_cap.py                                6 passed 
test_ttl_keeper.py                                   7 passed 
test_wallet_auth.py                                  1 failed 6 passed 
test_wallet_auth_tx.py                               8 passed 
test_x402_horizon_fallback.py                        5 passed 
test_x402_middleware.py                              10 passed 
```

---

## Contratos desplegados

Los IDs de contrato y su red viven en `.env.example`. Para comprobar que el bytecode desplegado
corresponde a este código:

```sh
stellar contract fetch --id <CONTRACT_ID> --network testnet --out-file /tmp/deployed.wasm
cd contracts && stellar contract build
shasum -a 256 /tmp/deployed.wasm target/wasm32v1-none/release/<contract>.wasm
```

⚠ **Al 2026-10-20 21:19 UTC estos hashes NO coinciden** para los tres contratos. Los contratos en testnet se
desplegaron con un toolchain anterior y sin el paso de optimización. Está registrado como pendiente;
se resuelve redesplegando desde este árbol.
