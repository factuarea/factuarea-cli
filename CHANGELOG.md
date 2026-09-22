# Changelog

Todo cambio reseñable de la CLI de Factuarea se anota aquí. El formato sigue
[Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y el versionado,
[SemVer](https://semver.org/lang/es/): mientras la CLI está en `0.x` una rotura
viaja en una `minor`, y `1.0.0` queda reservado para la GA de la API pública
(ver [`docs/RELEASING.md`](docs/RELEASING.md)).

Este fichero arranca con la ventana de rotura del eje de empresa; las etiquetas
anteriores (hasta `v0.4.0`) no tienen entrada aquí.

## [Unreleased]

El artefacto de este repositorio YA viaja con el contrato del eje: `internal/spec/openapi.json` embebe el documento de **481 operaciones** en **396 paths** (digest canónico `95b30ff071c9e3aaf89d603d9c5c6e1075ff13e9d1a7819559c72fbc0a89dcf8`) y `internal/cmd/resources_gen.go` se ha regenerado entero desde él.

### Cambiado — rotura

- **Todo comando de un recurso de empresa gana el segmento de empresa.** En la
  v1, los recursos de una empresa cuelgan de `/v1/companies/{company}/…`: la
  empresa deja de deducirse en silencio de la credencial y pasa a viajar en la
  invocación, donde se ve, se registra y se puede auditar.

  ```console
  # Antes
  $ factuarea invoices list --json

  # Ahora
  $ factuarea invoices list acme_co --json
  ```

  Alcance: de las **481** operaciones del contrato, **438** cuelgan del eje de
  empresa, **38** del eje de cuenta (`/v1/accounts/{account}/…`, la cartera de
  una gestoría) y **5** no cuelgan de ninguno, así que la rotura toca casi todo
  el árbol. **Ningún comando cambia de nombre**: lo que cambia es cómo se
  invoca. Lo que se escribe en ese segmento es el `id` de la empresa —el que
  publica `factuarea account show` en `data.scope[].id`—, nunca su NIF.

  Ojo con el marcador: en las **8** operaciones de
  `/v1/accounts/{account}/companies/{company}` —las empresas de una cartera—
  `{company}` es el **recurso** y no el eje, así que ahí se escribe siempre. La
  CLI las distingue por el path, nunca por el nombre del parámetro.

  Una sola ventana de rotura: ver https://docs.factuarea.com/docs/changelog/axis-and-payload-breaking-window

  Esa entrada anuncia la rotura **entera** de la v1 —el re-anclaje de los
  recursos al eje de empresa y el cambio de payload que viaja con él: la fecha
  de cobro renombrada (`paid_at` → `paid_on`) y los importes, que pasan de
  número a cadena— como **una sola rotura en una sola ventana**. No son dos
  anuncios ni dos migraciones: quien adapte su integración una vez, la adapta
  entera. La entrada la publica el portal de documentación, que va después de
  este trabajo; esta nota solo la referencia y no la reproduce.

### Añadido

- **Tres formas de decir la empresa, y una de no decirla**, para que la
  mitigación esté en el mismo sitio que la rotura:

  ```bash
  # Forma larga: la empresa como primer argumento posicional.
  factuarea invoices show acme_co inv_01931b3e --json

  # La misma, con la flag persistente --company (vale en cualquier comando del árbol).
  factuarea invoices show --company acme_co inv_01931b3e --json

  # Forma corta: si tu credencial alcanza UNA sola empresa, no escribes la empresa.
  factuarea invoices show inv_01931b3e --json
  ```

  La ayuda larga (`--help`) de todo comando con eje termina enseñando las dos
  formas y avisando de la tercera:

  ```
  Forma canónica:  factuarea invoices show [company] <invoice>
  Forma corta:     factuarea invoices show <invoice> --company <company>
  Si tu credencial alcanza una sola empresa, ni siquiera hace falta --company: se resuelve sola.
  ```

- **Resolución automática cuando la credencial alcanza un solo NIF**, tercer
  eslabón de una cadena de cuatro: argumento posicional → flag persistente
  `--company` → el ámbito de la credencial (`GET /v1/me`, campo `data.scope`)
  cuando alcanza **una sola** empresa → error accionable. El valor resuelto se
  recuerda durante esa invocación y **jamás se escribe en disco**: un valor
  persistido sobreviviría a la rotación de la clave o al cambio de perfil, y
  acabarías operando en silencio sobre una empresa fuera de tu ámbito. No
  existe `FACTUAREA_COMPANY` ni clave de empresa en `config.toml`, y su
  ausencia es una decisión, no un olvido.

- **Errores de uso accionables**, por stderr, con código de salida `2` y antes
  de ejecutar la operación. Cuando la credencial alcanza varias empresas, el
  mensaje enumera el ámbito —`id` a la izquierda, que es lo que la flag come;
  NIF y nombre a la derecha, que es lo que tú reconoces— para que la orden
  siguiente se copie y se pegue:

  ```console
  $ factuarea invoices show inv_01931b3e
  Error: tu credencial alcanza 2 empresas y no elijo por ti; pasa una como argumento posicional (<company>) o fíjala con --company:
    --company cmp_01931b3e7a2c   B12345678 — Acme Sillas SL
    --company cmp_01934c8d2e5f   B87654321 — Nordic Muebles SL
  ```

  Aportar la empresa por las dos vías a la vez también es error de uso: la CLI
  no elige por ti. Y en las operaciones irreversibles que actúan sobre la
  empresa **entera**, `--company` no la rellena a propósito, porque confirmar
  sobre un identificador que no has escrito no confirma nada.

- **El hueco de la empresa se publica aparte para los agentes**:
  `factuarea commands --json` lo declara en `optional_args`
  (`{"company": "--company"}`), sin mover el orden ni los nombres de `args`.

- **Superficie nueva del contrato del eje: 17 operaciones** respecto al contrato
  anterior (15 del eje de cuenta —`members`, `invitations`, `claim-tokens`,
  `owner transfer`, `usage`, `module-access`—, más
  `companies issuing-readiness` y `recurring-invoices bulk-status`). Es
  superficie **añadida**: no consume ventana de rotura, y ya está en el árbol de
  comandos.

- **Las cinco operaciones de API keys se mudan al eje de cuenta.** Desaparecen
  `factuarea companies api-keys {list,create,show,revoke,rotate-secret}` y las
  publica `factuarea account api-keys …` sobre `/v1/accounts/{account}/api-keys`:
  una clave alcanza una cartera, no una empresa suelta.

- **El devloop y el arranque de sesión se re-anclan al contrato.** `listen`,
  `trigger` y la comprobación de `login` consultaban rutas planas que la v1 ya no
  declara: la identidad de la credencial pasa de `/v1/account` a `/v1/me` (y se
  lee del propio contrato embebido, no de un literal), y el feed de eventos y los
  fixtures de `trigger` cuelgan ya de `/v1/companies/{company}/…`, resolviendo la
  empresa por la MISMA cadena de precedencia que el árbol generado.

[Unreleased]: https://github.com/factuarea/factuarea-cli/compare/v0.4.0...HEAD
