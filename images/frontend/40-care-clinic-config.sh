#!/bin/sh
# Fills the clinic's frontend settings into the CARE bundle before nginx starts.
set -eu
exec care-clinic-configure \
  -template /care-clinic/html \
  -out /usr/share/nginx/html \
  -keys /care-clinic/runtime-keys.json \
  -env /care-clinic/frontend.env
