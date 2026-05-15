# Netio - Custom HTTP Framework in Go

## Estrutura do projeto

```
./context.go
./context_test.go
./cors/cors.go
./cors/cors_test.go
./e2e/cmd/main.go
./e2e/http_netio_test.go
./e2e/https_netio_test.go
./helpers_test.go
./https.go
./logger.go
./logger_test.go
./netio.go
./netio_test.go
./netio_integration_test.go
./parse.go
./parse_test.go
./response_writer.go
./response_writer_test.go
./router_group.go
./router_group_test.go
./router_group_integration_test.go
./router_node.go
./router_node_test.go
./split.go
./split_test.go
```

---

## Issue em aberto: CORS headers não aparecem no Atendi9-API

### Contexto
- O netio **funciona corretamente isolado** — testamos com curl local e todos os headers CORS aparecem
- Quando integrado no **Atendi9-API** (deploy no Heroku), os headers CORS **não aparecem** na resposta
- A equipe voltou pro **Fiber** temporariamente pra não travar a homologação

### Possíveis causas corrigidas
- **Middleware duplicado** — `chain()` e `serve()` ambos prepend `a.mw` (corrigido)
- **Header lookup case-sensitive** — `header()` interno usava `bytes.Equal` exato (corrigido — headers normalizados pra lowercase)
- **Reason phrase errada** — todas as responses tinham "OK" (corrigido — usa `http.StatusText()`)
- **Vary header sobrescrito** — CORS middleware usava `HeaderSet` em vez de `HeaderAppend` (corrigido)

### Servidor de teste local
```bash
go run ./e2e/cmd/
curl -s -D - -X OPTIONS \
  -H "Origin: https://homologaatendi9.netlify.app" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: apikey,authorization" \
  http://localhost:3333/v1/dashboard/test@test.com/all
```

---

## Cobertura de testes atual

**Cobertura total:** 93.6% (netio package: 97.0%, cors: 100%)

### Gaps que não valem cobrir
- `New` — branches de `net.Listen`/`net.SplitHostPort` falhando (erros de SO)
- `Listen` — branch de `net.Listen` falhando (erro de SO)
- `ListenHTTPS` — mesmos erros de SO
- `generateMaxBodySize` — `strconv.Atoi` nunca falha porque input já validado
- `SendFileFromReader` — write error branches requerem conexão morta mid-stream
- `parseBody`/`parseChunked` — `io.ReadFull` error requer desconexão mid-read

---

## Testes

### Rodar testes

```bash
# só unitários
go test ./...

# unitários + integração
go test ./... -tags=integration
```

### Separação unitário vs integração

Arquivos com build tag `//go:build integration`:
- `netio_integration_test.go` — `serveOneRequest` helper + teste com TCP real
- `router_group_integration_test.go` — `TestGroup_Get` com `net.Pipe`

Arquivos sem build tag (unitários puros):
- `netio_test.go`, `helpers_test.go`, `context_test.go`, `parse_test.go`
- `response_writer_test.go`, `router_group_test.go`, `router_node_test.go`
- `split_test.go`, `logger_test.go`, `cors/cors_test.go`

### Leitura de coverage

```bash
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html && explorer.exe $(wslpath -w $(pwd)/coverage.html)
```

---

## Convenções do projeto

### Headers normalizados pra lowercase
Headers de request normalizados pra lowercase durante o parsing em `parseHeaders()`.
Lookup público `Context.Header("Content-Type")` aceita qualquer case.
Lookup interno `header(c, []byte("content-length"))` exige lowercase.

### Body size check antes de alocar
`parseBody` e `parseChunked` checam `maxBodySize` **antes** de fazer `make([]byte, n)`, evitando alocação de bodies enormes.

### Config do cors
`AllowedOrigins`, `AllowedMethods`, `AllowedHeaders`, `ExposedHeaders`, `MaxAgeSeconds`, `AllowCredentials`

### Test doubles compartilhados (helpers_test.go)
`fakeConn`, `badRemoteAddrConn`, `errorReader`

---

## Decisões de design

### Router: static > param
Rotas estáticas têm prioridade sobre param nodes. `/users/count` é preferido sobre `/users/:id`.

### Shutdown graceful
`Shutdown()` fecha o listener, espera conexões ativas, e force-close as que sobram quando o ctx expira.

### New() com porta auto
`New(AppConfig{})` sem porta mantém o listener aberto (evita TOCTOU). `Listen()` reutiliza.

### Read deadline anti-slowloris
Cada request tem deadline de 60s no read. Conexões lentas são fechadas automaticamente.

### Double-write guard
`writeResponseWithHeaders` é no-op se `ctx.wrote == true`. Múltiplas chamadas a `Send()`/`JSON()` não corrompem o stream HTTP.

### Transfer-Encoding vs Content-Length
`writeResponseWithHeaders` não emite `Content-Length` quando `Transfer-Encoding` está presente (RFC 7230).

### Vary header append
CORS middleware usa `HeaderAppend` em vez de `HeaderSet` pra não sobrescrever `Vary` existente.

### Nested groups sem slice aliasing
`group.Group()` copia o slice de middlewares explicitamente, evitando mutação do parent.

✻ Conversation compacted (ctrl+o for history)

  ⎿  Referenced file netio_test.go
  ⎿  Referenced file context_test.go
  ⎿  Read parse_test.go (214 lines)
  ⎿  Read cors/cors.go (99 lines)
  ⎿  Read context.go (365 lines)
  ⎿  You've hit your limit · resets 12am (America/Cayenne)

✻ Brewed for 1m 47s

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯  
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏸ plan mode on (shift+tab to cycle)
