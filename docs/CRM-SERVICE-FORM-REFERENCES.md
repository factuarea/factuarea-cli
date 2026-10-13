# Referencias actuales para formularios de servicio

```sh
factuarea crm capture-forms service-form-references --json
factuarea commands --json
```

El comando consume la lectura nativa `GET /v1/crm/capture/service-form-references`, operación `crm_capture_forms.references` y `operationId` `crmGetServiceFormReferences`. No acepta argumentos, consulta, cuerpo, paginación ni clave de idempotencia. Usa el cliente, perfil y credencial existentes del CLI; la credencial y el contexto autenticado del servidor fijan la empresa actual y el entorno. El scope fino y el scope OAuth del productor son `crm_capture_forms:read`. La resolución de credenciales del CLI actual admite API keys: este incremento conserva ese mecanismo y no añade un login OAuth.

El envelope `data` contiene las colecciones obligatorias `queues`, `policies`, `calendars`, `responsible_users` y la cadena `authorization_version`. Colas, políticas y responsables publican únicamente `id` y `name`; los calendarios añaden `version`. `id` es el alias V1 del UUID nativo: UUIDv7 canónico para colas, políticas y calendarios; UUIDv4/v7 canónico para usuarios. La salida conserva los nombres y enteros originales, incluidos calendarios con versiones mayores que el rango exacto de un número JavaScript. Las colecciones pueden estar vacías; cada una contiene como máximo 100 opciones y ninguna crea opciones predeterminadas. La política y el calendario se eligen de forma independiente.

El servidor revalida los permisos, C7, membresías y máscaras actuales de los dueños antes y después de leer las opciones. Conocer un ID o una `authorization_version` no concede acceso ni aprueba la publicación de un formulario. El CLI valida el contrato cerrado antes de mostrar la respuesta: rechaza campos privados, aliases SPA, claves de instalación, identidades duplicadas, valores ausentes o incompatibles y nombres vacíos. Conserva los errores nativos y realiza una sola petición de referencias, sin fallback SPA ni reconstrucción de permisos.

`commands --json` descubre el comando, ruta, método, scope y schema de respuesta sin consultar la API. El registro del comando es código fuente con activación OFF: la disponibilidad depende de la composición y los controles actuales del servidor. Esta lectura de opciones actuales no constituye un recibo ni verifica una intención anterior.

Las pruebas de este incremento usan exclusivamente servidores HTTP locales con credenciales sintéticas. No acreditan una ejecución del backend, su base de datos, el despliegue o la disponibilidad del endpoint.
