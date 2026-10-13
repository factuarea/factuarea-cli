# Conversaciones CRM: lecturas nativas

Dos comandos usan los carriers V1 Conversation NativeReadSurfacesBatch75:

```sh
factuarea crm conversations list --json
factuarea crm conversations list --filters '{ "mailbox_uuid":"019c0511-1111-7111-8111-222222222222", "state":"active", "search":"consulta", "sort":"last_message_at:desc" }' --limit 20 --json
factuarea crm conversations get 019c0511-1111-7111-8111-111111111111 --json
factuarea crm conversations get 019c0511-1111-7111-8111-111111111111 --cursor '<next_cursor original>' --limit 20 --json
factuarea crm conversations list --filters '{"state":"active"}' --all --json
```

List ejecuta GET /v1/crm/conversation (`listCrmConversations`, operación `conversations.list`). Get ejecuta GET /v1/crm/conversation/{conversation} (`getCrmConversation`, operación `conversations.get`). Se reutilizan credencial, perfil, entorno, resolución de scopes, cliente y salida existentes. No se envía body, Idempotency-Key ni confirmación; cada llamada usa el transporte nativo de un solo intento, sin redirecciones ni fallback.

El scope fino es `conversations:read`. El guard actual de Conversation exige también `crm:read` o `*`. Ambos se comprueban con los scopes de la credencial existente; `crm:write` ni `conversations.read` sustituyen esos scopes finos. En el Source V1 inspeccionado no existe entrada literal crm:read/crm.read en el catálogo; una credencial vigente `*` satisface ambos y conversations:read solo se deniega. La CLI no crea esa concesión ni decide disponibilidad, plan, consentimiento OAuth o acceso al proveedor.

`--filters` conserva exactamente el string JSON y lo codifica para URL una vez. Sus únicos miembros nativos son mailbox_uuid, state (active|archived), search (no vacío hasta 255 caracteres), target_type y target_uuid juntos, y sort (subject|last_message_at:asc|desc). target_type acepta contact/person/lead/opportunity/quote/invoice/proposal/activity. Identidades de mailbox/target/conversation son UUIDv7; UUID históricos de User se conservan en owner_id. Los nombres mailbox_uuid y target_uuid dentro de filters pertenecen al parser propietario y se mantienen aunque la respuesta use *_id. Se rechazan duplicados JSON, arrays, miembros extra y selección administrativa antes de consultar red.

`--cursor` conserva el cursor cifrado/autenticado original; requiere la misma selección, límite, canal, credencial, empresa, actor, sandbox, conexión, C7, máscaras y revisión del buzón que decide el dueño. `--limit` es decimal canónico 1..100. Si se omiten filters/cursor/limit, la CLI deja al servidor aplicar {} / null / 20. Get no acepta filters.

Sin `--all`, stdout conserva el envelope JSON nativo de una página, incluidos data.next_cursor y meta.authorization_version/readable_fields/filterable_fields/sortable_fields. `--all` sigue exclusivamente data.next_cursor y devuelve un array JSON de esos envelopes. Cada página mantiene su metadata propia. La continuación debe conservar C7 y las máscaras ligadas al cursor anterior; un cambio, cursor repetido, respuesta inválida o rechazo posterior retira todo stdout de esa ejecución. No se suma total/active_count/archived_count ni se fabrica un snapshot o cursor. El servidor reautoriza cada petición; la metadata anterior no concede acceso a la siguiente.

Las identidades públicas son id, mailbox_id, owner_id, team_id, conversation_id, message_id y links[].id. Las claves de `meta` siguen usando los campos nativos uuid/mailbox_uuid/owner_uuid/team_uuid. La CLI valida el envelope cerrado y sus tipos antes de imprimir: máscaras privadas permanecen null; links/messages/attachments ocultos permanecen arrays vacíos. Un campo privado incompatible con readable_fields se rechaza con 502 invalid_native_response. Se preservan enteros int64 sin conversión a float, estado, versión y fechas UTC originales.

Adjuntos son metadata de mensajes visibles de esta página: id/conversation_id/message_id/version/filename/mime/size/status. Su estado quarantine/ready/rejected se conserva; la CLI no publica URL, storage key, bytes ni acceso a un adjunto ajeno. Los estados de entrega pending/confirmed/unconfirmed/failed pertenecen a Mail. send_intent no prueba entrega; unconfirmed se conserva y no se convierte en éxito confirmado. Una entrega confirmed requiere submitted_at original; ocultar delivery_status oculta también submitted_at.

Errores nativos conservan status/code/subcode y el contrato existente de stderr/exit: 401→3, 403→4, 422→5, 404→6, 429→7, 409→8, 500/503→9; fallos de transporte→10. Errores de uso son 2. No se imprime una representación parcial ni se consulta SPA/recovery/otra ruta para sustituir una denegación. Las rutas publicadas pueden permanecer OFF según el catálogo; esta entrega de CLI no activa flags/grants/release.

Este incremento aporta los dos métodos reales al inventario, comandos, output/paginación y pruebas de las tareas originales 2.1/2.2/2.3/2.5 de crm-cli-delivery (CRM-082). Literal 2.1: «Inventario comandos de todas las operacionesv1 CRM, parámetros/UUID/filtros/cursor/scopes y confirmaciones.» La contribución es list/get Conversation75 y su evidencia offline; no completa por sí sola «todas las operacionesv1 CRM» ni cambia ticks globales. Backend, DB, montaje de carriers, grants y release pertenecen a sus entregas.
