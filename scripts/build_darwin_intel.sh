#!/bin/sh

# Ocelot Intel macOS 12 build lane.
#
# This is intentionally separate from scripts/build_darwin.sh so the existing
# Apple Silicon and universal Darwin defaults stay unchanged. It builds a
# single darwin/amd64 payload with a macOS 12.0 deployment target. Validate
# finished packages on the rogue Intel Mac before distribution.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

VERSION=${VERSION:-$(git -C "$REPO_ROOT" describe --tags --first-parent --abbrev=7 --long --dirty --always | sed -e "s/^v//g")}
DIST_DIR=${DIST_DIR:-"$REPO_ROOT/dist"}
BUILD_DIR=${BUILD_DIR:-"$REPO_ROOT/build/darwin-amd64-macos12"}
SOURCE_BUILD_DIR=${SOURCE_BUILD_DIR:-"$REPO_ROOT/build/darwin-intel-sources"}
INSTALL_PREFIX=${INSTALL_PREFIX:-"$DIST_DIR/darwin-amd64-macos12"}
MACOSX_DEPLOYMENT_TARGET=${MACOSX_DEPLOYMENT_TARGET:-12.0}
OCELOT_INTEL_ENABLE_MLX=${OCELOT_INTEL_ENABLE_MLX:-0}
OCELOT_ARTIFACT_PREFIX=${OCELOT_ARTIFACT_PREFIX:-ocelot}
OCELOT_INTEL_RUNTIME_BASENAME=${OCELOT_INTEL_RUNTIME_BASENAME:-"$OCELOT_ARTIFACT_PREFIX-darwin-amd64-macos12-$VERSION"}
OCELOT_INTEL_DMG_NAME=${OCELOT_INTEL_DMG_NAME:-"Ocelot-intel-macos12-$VERSION.dmg"}
OCELOT_INTEL_APP_ZIP_NAME=${OCELOT_INTEL_APP_ZIP_NAME:-"Ocelot-intel-macos12-$VERSION.zip"}
OCELOT_INTEL_APP_NAME=${OCELOT_INTEL_APP_NAME:-"Ocelot Intel"}
OCELOT_INTEL_BUNDLE_ID=${OCELOT_INTEL_BUNDLE_ID:-"ai.ocelot.ocelot.intel"}
OCELOT_INTEL_VOL_NAME=${OCELOT_INTEL_VOL_NAME:-"$OCELOT_INTEL_APP_NAME"}
OCELOT_INTEL_CREATE_DMG=${OCELOT_INTEL_CREATE_DMG:-1}

export MACOSX_DEPLOYMENT_TARGET
export CGO_CFLAGS=${CGO_CFLAGS:-"-O3 -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"}
export CGO_CXXFLAGS=${CGO_CXXFLAGS:-"-O3 -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"}
export CGO_LDFLAGS=${CGO_LDFLAGS:-"-mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"}

status() { echo >&2 ">>> $*"; }
fail() { echo >&2 "ERROR: $*"; exit 1; }

usage() {
    cat <<EOF
usage: $(basename "$0") [build package app sign dry-run help]

Commands:
  build      Build the single-arch Ocelot darwin/amd64 runtime payload.
  package    Create $OCELOT_INTEL_RUNTIME_BASENAME.tgz from the runtime payload.
  app        Build an Ocelot-named Intel app bundle and zip/dmg artifacts.
  sign       Sign the runtime payload if APPLE_IDENTITY is set, then package it.
  dry-run    Print the lane configuration without building.
  help       Show this help.

Environment overrides:
  VERSION                         Version embedded in binaries and artifact names.
  DIST_DIR                        Output root. Default: $DIST_DIR
  BUILD_DIR                       CMake build directory. Default: $BUILD_DIR
  INSTALL_PREFIX                  Runtime staging directory. Default: $INSTALL_PREFIX
  MACOSX_DEPLOYMENT_TARGET        Deployment target. Default: 12.0
  OCELOT_INTEL_ENABLE_MLX=1       Opt in to experimental x86_64 MLX metal_v3.
  OCELOT_INTEL_RUNTIME_BASENAME   Runtime tarball basename.
  OCELOT_INTEL_APP_NAME           App bundle display name. Default: Ocelot Intel
  OCELOT_INTEL_DMG_NAME           DMG artifact name.
  OCELOT_INTEL_APP_ZIP_NAME       Zip artifact name.

This lane is x86_64-only and must be validated on the rogue Intel Mac.
EOF
}

is_truthy() {
    case "${1:-}" in
        1|true|TRUE|yes|YES|on|ON) return 0 ;;
        *) return 1 ;;
    esac
}

guardrails() {
    status "Ocelot Intel macOS 12 lane: GOOS=darwin GOARCH=amd64 CMAKE_OSX_ARCHITECTURES=x86_64"
    status "Deployment target: macOS $MACOSX_DEPLOYMENT_TARGET"
    status "Validate completed artifacts on the rogue Intel Mac before distributing."

    host_arch=$(uname -m 2>/dev/null || echo unknown)
    if [ "$host_arch" != "x86_64" ]; then
        status "Current host architecture is $host_arch; this lane cross-builds amd64 but runtime smoke testing belongs on rogue."
    fi

    if is_truthy "$OCELOT_INTEL_ENABLE_MLX"; then
        status "Experimental x86_64 MLX support is enabled by OCELOT_INTEL_ENABLE_MLX=$OCELOT_INTEL_ENABLE_MLX."
    else
        status "MLX x86_64 support is off by default. Set OCELOT_INTEL_ENABLE_MLX=1 to opt in."
    fi
}

print_config() {
    cat <<EOF
repo: $REPO_ROOT
version: $VERSION
dist_dir: $DIST_DIR
build_dir: $BUILD_DIR
source_build_dir: $SOURCE_BUILD_DIR
install_prefix: $INSTALL_PREFIX
runtime_tgz: $DIST_DIR/$OCELOT_INTEL_RUNTIME_BASENAME.tgz
app_bundle: $DIST_DIR/$OCELOT_INTEL_APP_NAME.app
app_zip: $DIST_DIR/$OCELOT_INTEL_APP_ZIP_NAME
dmg: $DIST_DIR/$OCELOT_INTEL_DMG_NAME
bundle_id: $OCELOT_INTEL_BUNDLE_ID
deployment_target: $MACOSX_DEPLOYMENT_TARGET
goos: darwin
goarch: amd64
cmake_arch: x86_64
mlx_enabled: $OCELOT_INTEL_ENABLE_MLX
EOF
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required for this lane"
}

require_notarization_env() {
    missing=""
    for name in APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID; do
        eval "value=\${$name:-}"
        if [ -z "$value" ]; then
            missing="${missing}${missing:+, }$name"
        fi
    done
    [ -z "$missing" ] || fail "Notarization requires $missing when APPLE_IDENTITY is set"
}

clean_stale_mlx_payload() {
    if is_truthy "$OCELOT_INTEL_ENABLE_MLX"; then
        return 0
    fi

    mlx_payload_dir="$INSTALL_PREFIX/lib/ollama"
    [ -d "$mlx_payload_dir" ] || return 0

    for stale_mlx in "$mlx_payload_dir"/mlx_*; do
        [ -e "$stale_mlx" ] || continue
        case "$stale_mlx" in
            "$mlx_payload_dir"/mlx_*) ;;
            *) fail "refusing to remove unexpected MLX path: $stale_mlx" ;;
        esac

        status "Removing stale MLX payload from non-MLX build: $stale_mlx"
        if [ -d "$stale_mlx" ]; then
            rm -R "$stale_mlx"
        else
            rm "$stale_mlx"
        fi
    done
    return 0
}

prepare_sources() {
    if is_truthy "$OCELOT_INTEL_ENABLE_MLX"; then
        status "Preparing shared llama.cpp and MLX sources"
        cmake -S "$REPO_ROOT" -B "$SOURCE_BUILD_DIR" \
            -DOLLAMA_MLX_BACKENDS=metal_v3 \
            -DOLLAMA_LLAMA_BACKENDS=
        cmake --build "$SOURCE_BUILD_DIR" --target ollama-llama-cpp-source --target ollama-mlx-sources
    else
        status "Preparing shared llama.cpp sources"
        cmake -S "$REPO_ROOT" -B "$SOURCE_BUILD_DIR" \
            -DOLLAMA_MLX_BACKENDS= \
            -DOLLAMA_LLAMA_BACKENDS=
        cmake --build "$SOURCE_BUILD_DIR" --target ollama-llama-cpp-source
    fi
}

configure_build() {
    LLAMA_CPP_SHARED_SRC="$SOURCE_BUILD_DIR/_deps/llama_cpp-src"
    [ -d "$LLAMA_CPP_SHARED_SRC" ] || fail "missing llama.cpp source directory: $LLAMA_CPP_SHARED_SRC"

    mkdir -p "$DIST_DIR" "$INSTALL_PREFIX"

    if is_truthy "$OCELOT_INTEL_ENABLE_MLX"; then
        MLX_SHARED_SRC="$SOURCE_BUILD_DIR/_deps/mlx-src"
        MLX_C_SHARED_SRC="$SOURCE_BUILD_DIR/_deps/mlx-c-src"
        [ -d "$MLX_SHARED_SRC" ] || fail "missing MLX source directory: $MLX_SHARED_SRC"
        [ -d "$MLX_C_SHARED_SRC" ] || fail "missing MLX-C source directory: $MLX_C_SHARED_SRC"

        status "Configuring Ocelot darwin/amd64 with optional MLX metal_v3"
        cmake -S "$REPO_ROOT" -B "$BUILD_DIR" \
            -DCMAKE_BUILD_TYPE=Release \
            -DCMAKE_OSX_ARCHITECTURES=x86_64 \
            -DCMAKE_OSX_DEPLOYMENT_TARGET="$MACOSX_DEPLOYMENT_TARGET" \
            -DCMAKE_INSTALL_PREFIX="$INSTALL_PREFIX" \
            -DOLLAMA_PAYLOAD_INSTALL_PREFIX="$INSTALL_PREFIX" \
            -DOLLAMA_GO_OUTPUT="$INSTALL_PREFIX/bin/ocelot" \
            -DOLLAMA_VERSION="$VERSION" \
            -DOLLAMA_MLX_BACKENDS=metal_v3 \
            -DOLLAMA_LLAMA_BACKENDS= \
            -DMLX_ENABLE_X64_MAC=ON \
            -DFETCHCONTENT_SOURCE_DIR_LLAMA_CPP="$LLAMA_CPP_SHARED_SRC" \
            -DFETCHCONTENT_SOURCE_DIR_MLX="$MLX_SHARED_SRC" \
            -DFETCHCONTENT_SOURCE_DIR_MLX-C="$MLX_C_SHARED_SRC"
    else
        status "Configuring Ocelot darwin/amd64 without MLX"
        cmake -S "$REPO_ROOT" -B "$BUILD_DIR" \
            -DCMAKE_BUILD_TYPE=Release \
            -DCMAKE_OSX_ARCHITECTURES=x86_64 \
            -DCMAKE_OSX_DEPLOYMENT_TARGET="$MACOSX_DEPLOYMENT_TARGET" \
            -DCMAKE_INSTALL_PREFIX="$INSTALL_PREFIX" \
            -DOLLAMA_PAYLOAD_INSTALL_PREFIX="$INSTALL_PREFIX" \
            -DOLLAMA_GO_OUTPUT="$INSTALL_PREFIX/bin/ocelot" \
            -DOLLAMA_VERSION="$VERSION" \
            -DOLLAMA_MLX_BACKENDS= \
            -DOLLAMA_LLAMA_BACKENDS= \
            -DFETCHCONTENT_SOURCE_DIR_LLAMA_CPP="$LLAMA_CPP_SHARED_SRC"
    fi
}

build_runtime() {
    guardrails
    require_command cmake
    require_command go
    prepare_sources
    configure_build
    clean_stale_mlx_payload

    status "Building Ocelot darwin/amd64 runtime payload"
    if is_truthy "$OCELOT_INTEL_ENABLE_MLX"; then
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
            cmake --build "$BUILD_DIR" --target ollama-local --target ollama-mlx-backends --parallel
    else
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
            cmake --build "$BUILD_DIR" --target ollama-local --parallel
    fi

    [ -x "$INSTALL_PREFIX/bin/ocelot" ] || fail "missing built Ocelot CLI: $INSTALL_PREFIX/bin/ocelot"
    [ -x "$INSTALL_PREFIX/lib/ollama/llama-server" ] || fail "missing llama-server payload"
    [ -x "$INSTALL_PREFIX/lib/ollama/llama-quantize" ] || fail "missing llama-quantize payload"
}

package_runtime() {
    guardrails
    [ -x "$INSTALL_PREFIX/bin/ocelot" ] || fail "run '$0 build' first; missing $INSTALL_PREFIX/bin/ocelot"
    [ -d "$INSTALL_PREFIX/lib/ollama" ] || fail "run '$0 build' first; missing $INSTALL_PREFIX/lib/ollama"

    mkdir -p "$DIST_DIR"
    runtime_tar="$DIST_DIR/$OCELOT_INTEL_RUNTIME_BASENAME.tar"
    runtime_tgz="$DIST_DIR/$OCELOT_INTEL_RUNTIME_BASENAME.tgz"

    status "Creating Ocelot Intel runtime tarball: $runtime_tgz"
    tar -cf "$runtime_tar" -C "$INSTALL_PREFIX" bin lib
    gzip -9vc <"$runtime_tar" >"$runtime_tgz"
}

sign_runtime() {
    guardrails
    if [ -n "${APPLE_IDENTITY:-}" ]; then
        status "Signing Ocelot Intel runtime payload"
        find "$INSTALL_PREFIX/bin" "$INSTALL_PREFIX/lib/ollama" -type f \( -perm -111 -o -name "*.dylib" -o -name "*.so" -o -name "*.metallib" \) -print |
            while IFS= read -r artifact; do
                codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier "$OCELOT_INTEL_BUNDLE_ID" --options=runtime "$artifact"
            done
    else
        status "APPLE_IDENTITY is not set; skipping codesign."
    fi
    package_runtime
}

build_app() {
    guardrails
    require_command go
    require_command npm
    require_command ditto
    require_command plutil

    [ -x "$INSTALL_PREFIX/bin/ocelot" ] || fail "run '$0 build' first; missing $INSTALL_PREFIX/bin/ocelot"
    [ -d "$INSTALL_PREFIX/lib/ollama" ] || fail "run '$0 build' first; missing $INSTALL_PREFIX/lib/ollama"

    app_dir="$DIST_DIR/$OCELOT_INTEL_APP_NAME.app"
    if [ -e "$app_dir" ]; then
        fail "$app_dir already exists; move it aside before rebuilding the app bundle"
    fi

    status "Building app UI"
    (
        cd "$REPO_ROOT/app/ui/app"
        npm install
        npm run build
    )

    status "Creating Ocelot Intel app bundle: $app_dir"
    mkdir -p "$DIST_DIR"
    cp -a "$REPO_ROOT/app/darwin/Ollama.app" "$app_dir"
    touch "$app_dir"

    mkdir -p "$app_dir/Contents/MacOS"
    (
        cd "$REPO_ROOT"
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
            go build -o "$app_dir/Contents/MacOS/Ollama" \
            -ldflags="-s -w -X=github.com/ollama/ollama/app/version.Version=${VERSION}" \
            ./app/cmd/app
    )

    plutil -replace CFBundleDisplayName -string "$OCELOT_INTEL_APP_NAME" "$app_dir/Contents/Info.plist"
    plutil -replace CFBundleName -string "$OCELOT_INTEL_APP_NAME" "$app_dir/Contents/Info.plist"
    plutil -replace CFBundleIdentifier -string "$OCELOT_INTEL_BUNDLE_ID" "$app_dir/Contents/Info.plist"
    plutil -replace CFBundleShortVersionString -string "$VERSION" "$app_dir/Contents/Info.plist"
    plutil -replace CFBundleVersion -string "$VERSION" "$app_dir/Contents/Info.plist"
    plutil -replace LSMinimumSystemVersion -string "$MACOSX_DEPLOYMENT_TARGET" "$app_dir/Contents/Info.plist"

    mkdir -p "$app_dir/Contents/Frameworks/Squirrel.framework/Versions/A/Resources"
    cp -a "$app_dir/Contents/MacOS/Ollama" "$app_dir/Contents/Frameworks/Squirrel.framework/Versions/A/Squirrel"
    ln -s ../Squirrel "$app_dir/Contents/Frameworks/Squirrel.framework/Versions/A/Resources/ShipIt"
    cp -a "$REPO_ROOT/app/cmd/squirrel/Info.plist" "$app_dir/Contents/Frameworks/Squirrel.framework/Versions/A/Resources/Info.plist"
    ln -s A "$app_dir/Contents/Frameworks/Squirrel.framework/Versions/Current"
    ln -s Versions/Current/Resources "$app_dir/Contents/Frameworks/Squirrel.framework/Resources"
    ln -s Versions/Current/Squirrel "$app_dir/Contents/Frameworks/Squirrel.framework/Squirrel"

    mkdir -p "$app_dir/Contents/Resources"
    cp -a "$INSTALL_PREFIX/bin/ocelot" "$app_dir/Contents/Resources/ollama"
    cp -a "$INSTALL_PREFIX/lib/ollama/llama-server" "$app_dir/Contents/Resources/"
    cp -a "$INSTALL_PREFIX/lib/ollama/llama-quantize" "$app_dir/Contents/Resources/"
    cp -a "$INSTALL_PREFIX/lib/ollama/." "$app_dir/Contents/Resources/"
    chmod a+x "$app_dir/Contents/Resources/ollama"

    if [ -n "${APPLE_IDENTITY:-}" ]; then
        status "Signing Ocelot Intel app bundle"
        find "$app_dir/Contents/Resources" -type f \( -perm -111 -o -name "*.dylib" -o -name "*.so" -o -name "*.metallib" \) -print |
            while IFS= read -r artifact; do
                codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier "$OCELOT_INTEL_BUNDLE_ID" --options=runtime "$artifact"
            done
        codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier "$OCELOT_INTEL_BUNDLE_ID" --options=runtime "$app_dir/Contents/MacOS/Ollama"
        codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier "$OCELOT_INTEL_BUNDLE_ID" --deep --options=runtime "$app_dir"
    else
        status "APPLE_IDENTITY is not set; app bundle will be unsigned."
    fi

    app_zip="$DIST_DIR/$OCELOT_INTEL_APP_ZIP_NAME"
    status "Creating Ocelot Intel app zip: $app_zip"
    (
        cd "$DIST_DIR"
        ditto -c -k --norsrc --keepParent "$OCELOT_INTEL_APP_NAME.app" "$app_zip"
    )

    if is_truthy "$OCELOT_INTEL_CREATE_DMG"; then
        require_command "$SCRIPT_DIR/create-dmg.sh"
        dmg_path="$DIST_DIR/$OCELOT_INTEL_DMG_NAME"
        if [ -e "$dmg_path" ]; then
            fail "$dmg_path already exists; move it aside before rebuilding the dmg"
        fi

        status "Creating Ocelot Intel dmg: $dmg_path"
        (
            cd "$DIST_DIR"
            "$SCRIPT_DIR/create-dmg.sh" \
                --volname "$OCELOT_INTEL_VOL_NAME" \
                --volicon "$REPO_ROOT/app/darwin/Ollama.app/Contents/Resources/icon.icns" \
                --background "$REPO_ROOT/app/assets/background.png" \
                --window-pos 200 120 \
                --window-size 800 400 \
                --icon-size 128 \
                --icon "$OCELOT_INTEL_APP_NAME.app" 200 190 \
                --hide-extension "$OCELOT_INTEL_APP_NAME.app" \
                --app-drop-link 600 190 \
                --text-size 12 \
                "$OCELOT_INTEL_DMG_NAME" \
                "$OCELOT_INTEL_APP_NAME.app"
        )

        if [ -n "${APPLE_IDENTITY:-}" ]; then
            require_notarization_env
            codesign -f --timestamp -s "$APPLE_IDENTITY" --identifier "$OCELOT_INTEL_BUNDLE_ID" --options=runtime "$dmg_path"
            xcrun notarytool submit "$dmg_path" --wait --timeout 20m --apple-id "$APPLE_ID" --password "$APPLE_PASSWORD" --team-id "$APPLE_TEAM_ID"
            xcrun stapler staple "$dmg_path"
        fi
    fi
}

if [ "$#" -eq 0 ]; then
    build_runtime
    package_runtime
    exit 0
fi

for CMD in "$@"; do
    case "$CMD" in
        build) build_runtime ;;
        package) package_runtime ;;
        app) build_app ;;
        sign) sign_runtime ;;
        dry-run) guardrails; print_config ;;
        help|-h|--help) usage ;;
        *) usage; exit 1 ;;
    esac
done
