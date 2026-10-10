#!/usr/bin/env bash
# Checks a built CARE Clinic frontend image before it is published:
#   images/scripts/verify-frontend.sh <image>
# - every runtime setting was compiled as a placeholder,
# - two different clinics' settings produce different output with no
#   placeholders left behind,
# - nginx starts and serves the configured page.
set -euo pipefail

image=${1:?usage: verify-frontend.sh <image>}
prefix='https://care-clinic.invalid/__CARE_CLINIC_'
work=$(mktemp -d)
name="care-clinic-verify-$$"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

run() { docker run --rm --entrypoint sh "$@"; }

keys=$(run "$image" -c 'cat /care-clinic/runtime-keys.json' | node -e '
  let s = ""; process.stdin.on("data", d => s += d).on("end", () => console.log(Object.keys(JSON.parse(s)).join("\n")));')
[ -n "$keys" ] || { echo "image lists no runtime settings" >&2; exit 1; }

for key in $keys; do
  if ! run "$image" -c "grep -rqF '${prefix}${key}__' /care-clinic/html"; then
    echo "runtime setting $key is not in the compiled frontend; it was probably folded in at build time" >&2
    exit 1
  fi
done

render() { # <env file> <output dir>
  mkdir -p "$2"
  docker run --rm --user "$(id -u):$(id -g)" \
    -v "$1:/care-clinic/frontend.env:ro" -v "$2:/out" \
    --entrypoint care-clinic-configure "$image" -out /out
  if grep -rqF "$prefix" "$2"; then
    echo "placeholders left after configuring with $1" >&2
    exit 1
  fi
}

cat >"$work/a.env" <<'EOF'
REACT_CARE_API_URL=https://clinic-a.example
REACT_APP_TITLE=Clinic A
EOF
cat >"$work/b.env" <<'EOF'
REACT_CARE_API_URL=https://clinic-b.example
REACT_APP_TITLE=Clinic "B" <test>
EOF
render "$work/a.env" "$work/a"
render "$work/b.env" "$work/b"
grep -rqF 'https://clinic-a.example' "$work/a" || { echo "clinic A address missing" >&2; exit 1; }
grep -rqF 'https://clinic-b.example' "$work/b" || { echo "clinic B address missing" >&2; exit 1; }
if diff -rq "$work/a" "$work/b" >/dev/null; then
  echo "different settings produced identical output" >&2
  exit 1
fi

docker run -d --name "$name" -v "$work/a.env:/care-clinic/frontend.env:ro" -p 127.0.0.1::80 "$image" >/dev/null
port=$(docker port "$name" 80/tcp | head -n1 | sed 's/.*://')
for _ in $(seq 1 30); do
  if page=$(curl -fsS "http://127.0.0.1:$port/" 2>/dev/null); then
    if grep -qF 'Clinic A' <<<"$page"; then
      echo "ok: $image"
      exit 0
    fi
    echo "served page does not carry the configured title" >&2
    exit 1
  fi
  sleep 1
done
echo "nginx did not start" >&2
docker logs "$name" >&2 || true
exit 1
