#!/usr/bin/env bash
# Branchroom backup and restore drill on throwaway instances. It provisions a workspace and
# pushes on instance A, backs up with gitea dump plus the restricted root, restores to
# instance B at different paths, repairs hooks with gitea doctor and clones the work back.
# Needs curl, git, sqlite3 and unzip, and free ports 3301 and 3302.
# usage: GITEA=./gitea contrib/branchroom/restore-drill.sh
set -euo pipefail
umask 077
GITEA=$(readlink -f "${GITEA:-./gitea}")
D=${DRILL_DIR:-/tmp/branchroom-drill}
cleanup() { for p in "$D"/*/pid; do [ -f "$p" ] && kill "$(cat "$p")" 2>/dev/null || true; done; }
trap cleanup EXIT
rm -rf "$D" && mkdir -p "$D"
USER_NAME=drill
PASS='Dr1ll-pass-12345'
step() { echo "== $*"; }

write_ini() { # dir port
  mkdir -p "$1/custom/conf" "$1/log"
  [ -d "$1/restricted" ] || mkdir -m 0700 "$1/restricted"
  cat > "$1/custom/conf/app.ini" <<EOF
[database]
DB_TYPE = sqlite3
PATH = $1/data/gitea.db
[server]
HTTP_PORT = $2
ROOT_URL = http://localhost:$2/
[repository]
ROOT = $1/repos
[security]
INSTALL_LOCK = true
[log]
MODE = file
LEVEL = Warn
ROOT_PATH = $1/log
[branchroom]
ENABLED = true
RESTRICTED_ROOT = $1/restricted
EOF
}

start() { # dir port
  GITEA_WORK_DIR=$1 "$GITEA" web --config "$1/custom/conf/app.ini" > "$1/log/stdout.log" 2>&1 &
  echo $! > "$1/pid"
  for _ in $(seq 60); do curl -fsS "http://localhost:$2/api/healthz" >/dev/null 2>&1 && return 0; sleep 1; done
  cat "$1/log/stdout.log"; return 1
}

stop() { kill "$(cat "$1/pid")"; wait "$(cat "$1/pid")" 2>/dev/null || true; }

form() { # port path data... -> http status
  local port=$1 path=$2; shift 2
  curl -s -o /dev/null -w '%{http_code}' -b "$D/jar" -c "$D/jar" -X POST "http://localhost:$port$path" "$@"
}

login() { rm -f "$D/jar"; form "$1" /user/login -d user_name=$USER_NAME -d "password=$PASS" >/dev/null; }

clone_url() { # port
  curl -s -b "$D/jar" "http://localhost:$1/$USER_NAME/secret/-/branchroom" | grep -o 'id="branchroom-source-clone-0" value="[^"]*"' | sed 's/.*value="//;s/"$//;s/&amp;/\&/g'
}

with_creds() { echo "$1" | sed "s#http://#http://$USER_NAME:$PASS@#"; }

A=$D/a; B=$D/b
step "instance A"
write_ini "$A" 3301
start "$A" 3301
GITEA_WORK_DIR=$A "$GITEA" admin user create --config "$A/custom/conf/app.ini" --username $USER_NAME --password "$PASS" --email drill@example.invalid --admin --must-change-password=false >/dev/null
curl -fsS -u "$USER_NAME:$PASS" -H 'Content-Type: application/json' -d '{"name":"secret","auto_init":true}' http://localhost:3301/api/v1/user/repos >/dev/null
login 3301
S=/$USER_NAME/secret/settings/branchroom
[ "$(form 3301 "$S" -d name=embargo)" = 303 ]
SCOPE=$(curl -s -b "$D/jar" "http://localhost:3301$S" | grep -o 'name="scope_id" value="[0-9]*"' | head -1 | sed 's/.*value="\([0-9]*\)"/\1/')
[ -n "$SCOPE" ]
[ "$(form 3301 "$S/workspaces" -d scope_id="$SCOPE")" = 303 ]
[ "$(form 3301 "$S/source-grants" -d scope_id="$SCOPE" -d username=$USER_NAME)" = 303 ]
[ "$(form 3301 "$S/source-write-grants" -d scope_id="$SCOPE" -d username=$USER_NAME)" = 303 ]
URL_A=$(clone_url 3301)
echo "clone URL on A: ${URL_A%/*}/<route-key>.git"
git clone -q "$(with_creds "$URL_A")" "$D/work"
cd "$D/work"
git config user.name drill && git config user.email drill@example.invalid
echo "restricted marker $(date +%s)" > MARKER
head -c $((20 << 20)) /dev/urandom > big.bin
git add MARKER big.bin && git commit -qm 'restricted change with a 20 MiB file'
git push -q origin HEAD:refs/heads/branchroom/drill
echo "PASS: 20 MiB push through the gateway"
head -c $((40 << 20)) /dev/urandom > huge.bin
git add huge.bin && git commit -qm 'too large'
if git push origin HEAD:refs/heads/branchroom/too-large > "$D/oversize.log" 2>&1; then echo "FAIL: 40 MiB push was accepted"; exit 1; fi
grep -q 413 "$D/oversize.log" && echo "PASS: 40 MiB push refused with 413"
git reset -q --hard HEAD~1
MARKER=$(cat MARKER)
cd "$D"

step "backup: stop A, gitea dump, archive the restricted root"
stop "$A"
GITEA_WORK_DIR=$A "$GITEA" dump --config "$A/custom/conf/app.ini" --database sqlite3 --file "$D/dump.zip" >/dev/null 2>&1
tar -C "$A" -cpf "$D/restricted.tar" restricted
if unzip -l "$D/dump.zip" | grep -q 'restricted/'; then echo "FAIL: dump contains the restricted root"; exit 1; fi
echo "PASS: dump excludes restricted stores, archive holds $(tar -tf "$D/restricted.tar" | grep -c '\.git/$') store(s)"

step "restore into B at different paths"
mkdir -p "$B/unpacked" && unzip -q "$D/dump.zip" -d "$B/unpacked"
mkdir -p "$B/data" "$B/repos"
cp -a "$B/unpacked/data/." "$B/data/"
cp -a "$B/unpacked/repos/." "$B/repos/"
rm -f "$B/data/gitea.db" && sqlite3 "$B/data/gitea.db" < "$B/unpacked/gitea-db.sql"
tar -C "$B" -xpf "$D/restricted.tar"
write_ini "$B" 3302
start "$B" 3302

step "doctor on the restored instance"
if GITEA_WORK_DIR=$B "$GITEA" doctor check --config "$B/custom/conf/app.ini" --run branchroom 2>&1 | tee "$D/doctor1.log" | grep -q '^OK'; then
  echo "FAIL: doctor passed although the hook still points at instance A"; exit 1
fi
grep -q 'receive hook fails verification' "$D/doctor1.log" && echo "PASS: doctor reports the moved hook"
GITEA_WORK_DIR=$B "$GITEA" doctor check --config "$B/custom/conf/app.ini" --run branchroom --fix 2>&1 | grep -q '^OK'
GITEA_WORK_DIR=$B "$GITEA" doctor check --config "$B/custom/conf/app.ini" --run branchroom 2>&1 | grep -q '^OK'
echo "PASS: doctor --fix repaired the hook and a second run is clean"

step "restored source and push"
login 3302
URL_B=$(clone_url 3302)
git clone -q -b branchroom/drill "$(with_creds "$URL_B")" "$D/restored"
[ "$(cat "$D/restored/MARKER")" = "$MARKER" ] && [ "$(stat -c %s "$D/restored/big.bin")" = $((20 << 20)) ]
echo "PASS: restored workspace serves the pushed commit and file"
cd "$D/restored" && git config user.name drill && git config user.email drill@example.invalid
git commit -q --allow-empty -m 'after restore' && git push -q origin HEAD:refs/heads/branchroom/drill
echo "PASS: push after restore"
if git ls-remote "http://localhost:3302/$USER_NAME/secret.git" | grep -q branchroom/drill; then echo "FAIL: public repository advertises the restricted ref"; exit 1; fi
echo "PASS: public repository does not advertise the restricted ref"
stop "$B"
echo "DRILL OK"
