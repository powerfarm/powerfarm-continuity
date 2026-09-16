#!/bin/sh
echo "$@" >> "$(dirname "$0")/route-runs.log"
echo '{"verified": true}'
