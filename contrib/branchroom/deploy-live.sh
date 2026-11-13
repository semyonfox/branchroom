#!/usr/bin/env bash
# Replace the live Branchroom binary with the tip of agentic/research from the
# private GitHub repo. Builds first, then stops the service, backs up, swaps
# the binary and turns push protection on. A failed health check runs ROLLBACK.
# usage: branchroom-deploy
set -euo pipefail
umask 077

export PATH="${HOME}/.local/bin:${HOME}/.local/share/pnpm/bin:${PATH}"

repo=/home/semyon/code/owned/labs/gitea
live=/home/semyon/.local/share/branchroom
build=/home/semyon/.local/share/branchroom-build
branch=agentic/research
unit=branchroom.service

for cmd in git go node pnpm systemctl curl python3; do
	command -v "$cmd" >/dev/null
done

git -C "$repo" fetch --quiet branchroom "$branch"
sha=$(git -C "$repo" rev-parse FETCH_HEAD)
echo "deploying ${sha}"

if [ -d "$build/.git" ] || [ -f "$build/.git" ]; then
	git -C "$build" checkout --force --detach "$sha"
else
	git -C "$repo" worktree add --detach "$build" "$sha"
fi

# the next run should be this copy, including fixes made after the previous deploy
install -m 0755 "$build/contrib/branchroom/deploy-live.sh" "${HOME}/.local/bin/branchroom-deploy"

(
	cd "$build"
	TAGS=bindata make build
)

avail=$(df -B1 --output=avail "$live" | tail -1)
if [ "$avail" -lt $((40 * 1024 * 1024 * 1024)) ]; then
	echo "refusing to stop Branchroom, less than 40 GiB free" >&2
	exit 1
fi

stamp=$(date +%Y%m%d-%H%M)
backup="${HOME}/.local/share/branchroom-backups/${stamp}-${sha:0:12}"
short="g${sha:0:10}"

rollback() {
	echo "deploy failed, rolling back" >&2
	if [ -f "$backup/ROLLBACK" ]; then
		bash "$backup/ROLLBACK"
	else
		systemctl --user start "$unit" || true
	fi
}

systemctl --user stop "$unit"
if ! (
	set -euo pipefail
	mkdir -p "$backup/bin" "$backup/custom/conf"
	cp -a "$live/bin/gitea" "$backup/bin/gitea"
	cp -a "$live/data" "$backup/data"
	cp -a "$live/custom/conf/app.ini" "$backup/custom/conf/app.ini"
	tar -C "$live" -cpf "$backup/repositories.tar" repositories restricted-repositories
	cat > "$backup/ROLLBACK" <<EOF
systemctl --user stop branchroom.service
rm -rf ${live}/data ${live}/custom/conf/app.ini && cp -a ${backup}/data ${live}/data && cp -a ${backup}/custom/conf/app.ini ${live}/custom/conf/app.ini
cp -a ${backup}/bin/gitea ${live}/bin/gitea
rm -rf ${live}/repositories ${live}/restricted-repositories && tar -C ${live} -xpf ${backup}/repositories.tar
systemctl --user start branchroom.service
EOF
	install -m 0755 "$build/gitea" "$live/bin/gitea"
	python3 - <<'PY'
from pathlib import Path
p = Path("/home/semyon/.local/share/branchroom/custom/conf/app.ini")
text = p.read_text()
section = "[repository.secret_scanning]"
if section not in text:
    text = text.rstrip() + "\n\n[repository.secret_scanning]\nPUSH_PROTECTION = true\n"
else:
    head, tail = text.split(section, 1)
    rest, sep, following = tail.partition("\n[")
    if "PUSH_PROTECTION" in rest:
        lines = []
        replaced = False
        for line in rest.splitlines(keepends=True):
            if not replaced and line.strip().startswith("PUSH_PROTECTION"):
                lines.append("PUSH_PROTECTION = true\n")
                replaced = True
            else:
                lines.append(line)
        rest = "".join(lines)
        if not replaced:
            rest = "\nPUSH_PROTECTION = true\n" + rest
    else:
        rest = "\nPUSH_PROTECTION = true\n" + rest
    text = head + section + rest + (sep + following if sep else "")
p.write_text(text)
print("push protection on")
PY
	systemctl --user start "$unit"
	ok=0
	for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
		if curl -fsS --max-time 5 http://10.0.0.5:3080/api/healthz >/dev/null; then
			ok=1
			break
		fi
		sleep 2
	done
	[ "$ok" = 1 ]
	ver=$("$live/bin/gitea" --version)
	echo "$ver"
	case "$ver" in
	*"$short"*) ;;
	*) echo "running binary is not ${sha}" >&2; exit 1 ;;
	esac
); then
	rollback
	exit 1
fi

echo "live at ${sha}"
