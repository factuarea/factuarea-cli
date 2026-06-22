#!/bin/sh
# install.sh — Instalador endurecido de la CLI de Factuarea (curl | sh).
#
# Toda la lógica vive en funciones y `main "$@"` es la ÚLTIMA línea del
# fichero: si el stream `curl | sh` se corta a mitad de descarga, el shell
# no ejecuta nada (ninguna función se invoca hasta llegar al final).
#
# POSIX sh puro, sin bashismos. Verifica el checksum sha256 del archive
# contra checksums.txt y aborta si no coincide antes de instalar nada.
#
# Variables de entorno:
#   FACTUAREA_VERSION      tag a instalar (ej. v0.1.0). Default: último release.
#   FACTUAREA_INSTALL_DIR  destino del binario. Default: $HOME/.local/bin.
#   FACTUAREA_DRY_RUN=1    detecta OS/arch, resuelve versión y URLs, imprime y sale 0.

set -eu

REPO="factuarea/factuarea-cli"
PROJECT="factuarea"
BINARY="factuarea"

# --- salida ---------------------------------------------------------------

info() {
	printf '%s\n' "$*" >&2
}

error() {
	printf 'Error: %s\n' "$*" >&2
	exit 1
}

# --- utilidades -----------------------------------------------------------

has() {
	command -v "$1" >/dev/null 2>&1
}

# Descarga $1 a stdout usando curl o, en su defecto, wget.
fetch() {
	url="$1"
	if has curl; then
		curl -fsSL "$url"
	elif has wget; then
		wget -qO- "$url"
	else
		error "se necesita 'curl' o 'wget' para descargar; no se encontró ninguno."
	fi
}

# Descarga $1 al fichero $2.
fetch_to() {
	url="$1"
	dest="$2"
	if has curl; then
		curl -fsSL -o "$dest" "$url"
	elif has wget; then
		wget -qO "$dest" "$url"
	else
		error "se necesita 'curl' o 'wget' para descargar; no se encontró ninguno."
	fi
}

# --- detección de plataforma ---------------------------------------------

# Mapea uname -s a los valores de GoReleaser .Os (linux/darwin/windows).
detect_os() {
	os=$(uname -s)
	case "$os" in
		Linux) echo "linux" ;;
		Darwin) echo "darwin" ;;
		MINGW* | MSYS* | CYGWIN* | Windows_NT)
			error "Windows no se soporta vía este instalador. Usa 'npm i -g @factuarea/cli' o 'scoop install factuarea'."
			;;
		*)
			error "sistema operativo no soportado: $os"
			;;
	esac
}

# Mapea uname -m a los valores de GoReleaser .Arch (amd64/arm64).
detect_arch() {
	arch=$(uname -m)
	case "$arch" in
		x86_64 | amd64) echo "amd64" ;;
		aarch64 | arm64) echo "arm64" ;;
		*)
			error "arquitectura no soportada: $arch"
			;;
	esac
}

# --- resolución de versión ------------------------------------------------

# Último tag publicado, parseado de la API de releases sin jq.
latest_version() {
	api="https://api.github.com/repos/$REPO/releases/latest"
	tag=$(fetch "$api" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	if [ -z "$tag" ]; then
		error "no se pudo resolver el último release desde $api"
	fi
	echo "$tag"
}

# GoReleaser deriva .Version del tag quitando la 'v' inicial
# (name_template: {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}).
# Tag v0.1.0 -> archive factuarea_0.1.0_<os>_<arch>.tar.gz
strip_v() {
	echo "$1" | sed -E 's/^v//'
}

# --- verificación de checksum --------------------------------------------

# Imprime el sha256 de $1 (linux: sha256sum, macOS: shasum -a 256).
sha256_of() {
	file="$1"
	if has sha256sum; then
		sha256sum "$file" | awk '{print $1}'
	elif has shasum; then
		shasum -a 256 "$file" | awk '{print $1}'
	else
		error "se necesita 'sha256sum' o 'shasum' para verificar el checksum; no se encontró ninguno."
	fi
}

# Extrae de checksums.txt ($1) el sha256 esperado para el archive ($2).
expected_checksum() {
	checksums_file="$1"
	archive_name="$2"
	expected=$(grep " $archive_name\$" "$checksums_file" | awk '{print $1}')
	if [ -z "$expected" ]; then
		error "no se encontró el checksum de '$archive_name' en checksums.txt"
	fi
	echo "$expected"
}

# --- PATH -----------------------------------------------------------------

# Avisa con la línea para añadir $1 al PATH si no está ya.
warn_if_not_in_path() {
	dir="$1"
	case ":$PATH:" in
		*":$dir:"*) ;;
		*)
			info ""
			info "El directorio de instalación no está en tu PATH:"
			info "  $dir"
			info "Añádelo a tu shell (~/.bashrc, ~/.zshrc o equivalente):"
			info "  export PATH=\"$dir:\$PATH\""
			;;
	esac
}

# --- instalación ----------------------------------------------------------

install_cli() {
	os=$(detect_os)
	arch=$(detect_arch)

	if [ "${FACTUAREA_VERSION:-}" != "" ]; then
		tag="$FACTUAREA_VERSION"
	else
		info "Resolviendo el último release..."
		tag=$(latest_version)
	fi
	version=$(strip_v "$tag")

	archive="${PROJECT}_${version}_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/$tag"
	archive_url="$base/$archive"
	checksums_url="$base/checksums.txt"

	if [ "${FACTUAREA_DRY_RUN:-}" = "1" ]; then
		info "DRY RUN — no se descarga nada."
		info "OS detectado:    $os"
		info "Arch detectada:  $arch"
		info "Versión:         $tag"
		info "Archive:         $archive_url"
		info "Checksums:       $checksums_url"
		exit 0
	fi

	install_dir="${FACTUAREA_INSTALL_DIR:-$HOME/.local/bin}"

	tmpdir=$(mktemp -d 2>/dev/null || mktemp -d -t factuarea)
	trap 'rm -rf "$tmpdir"' EXIT INT TERM

	info "Descargando $archive..."
	fetch_to "$archive_url" "$tmpdir/$archive"

	info "Descargando checksums.txt..."
	fetch_to "$checksums_url" "$tmpdir/checksums.txt"

	info "Verificando checksum..."
	expected=$(expected_checksum "$tmpdir/checksums.txt" "$archive")
	actual=$(sha256_of "$tmpdir/$archive")
	if [ "$expected" != "$actual" ]; then
		error "el checksum no coincide. Esperado: $expected. Obtenido: $actual. Instalación abortada."
	fi
	info "Checksum verificado."

	info "Extrayendo..."
	tar -xzf "$tmpdir/$archive" -C "$tmpdir"
	if [ ! -f "$tmpdir/$BINARY" ]; then
		error "el archive no contiene el binario '$BINARY'."
	fi

	mkdir -p "$install_dir"
	install -m 0755 "$tmpdir/$BINARY" "$install_dir/$BINARY" 2>/dev/null ||
		{ cp "$tmpdir/$BINARY" "$install_dir/$BINARY" && chmod 0755 "$install_dir/$BINARY"; }

	info "Instalado: $install_dir/$BINARY ($tag)"
	warn_if_not_in_path "$install_dir"
}

main() {
	install_cli
}

main "$@"
