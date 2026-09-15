#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$ROOT/downloaded/src"
BUILD="$ROOT/runtime/build"
PREFIX="$ROOT/runtime/prefix"
mkdir -p "$BUILD" "$PREFIX"

for d in open62541 paho-mqtt-c libmodbus; do
  [[ -d "$SRC/$d" ]] || { echo "missing source $SRC/$d" >&2; exit 1; }
done

rm -rf "$BUILD/open62541"
cmake -S "$SRC/open62541" -B "$BUILD/open62541" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DUA_BUILD_EXAMPLES=OFF \
  -DUA_BUILD_UNIT_TESTS=OFF
cmake --build "$BUILD/open62541" -j2
cmake --install "$BUILD/open62541"

rm -rf "$BUILD/paho-mqtt-c"
cmake -S "$SRC/paho-mqtt-c" -B "$BUILD/paho-mqtt-c" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DPAHO_BUILD_SAMPLES=OFF \
  -DPAHO_ENABLE_TESTING=OFF \
  -DPAHO_WITH_SSL=OFF
cmake --build "$BUILD/paho-mqtt-c" -j2
cmake --install "$BUILD/paho-mqtt-c"

rm -rf "$BUILD/libmodbus"
cp -a "$SRC/libmodbus" "$BUILD/libmodbus"
(
  cd "$BUILD/libmodbus"
  ./autogen.sh
  ./configure --prefix="$PREFIX"
  make -j2
  make install
)

echo "industrial runtime ready in $PREFIX"
