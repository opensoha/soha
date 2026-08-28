#!/bin/sh
set -eu

interval="${SYNC_INTERVAL_SECONDS:-3}"
mode="${SYNC_MODE:-git}"
mkdir -p /workspace/.sync

case "$mode" in
	local)
		mkdir -p /workspace/soha /workspace/soha-web /workspace/soha-contracts
		printf 'local source sync mode; waiting for DevSpace\n'
		while :; do
			sleep 3600
		done
		;;
	git) ;;
	*) printf 'unknown source sync mode: %s\n' "$mode" >&2; exit 2 ;;
esac

sync_repository() {
	name="$1"
	repository="$2"
	ref="$3"
	path="/workspace/$name"
	marker="/workspace/.sync/$name"

	if [ ! -d "$path/.git" ]; then
		rm -rf "$path"
		mkdir -p "$path"
		git -C "$path" init --quiet
		git -C "$path" remote add origin "$repository"
	fi

	git -C "$path" fetch --quiet --depth=1 origin "$ref" || return 1
	new_revision="$(git -C "$path" rev-parse FETCH_HEAD)"
	old_revision="$(git -C "$path" rev-parse HEAD 2>/dev/null || true)"
	if [ "$new_revision" = "$old_revision" ]; then
		if [ ! -f "$marker" ]; then
			printf '%s\n' "$new_revision" > "$marker.next"
			mv "$marker.next" "$marker"
		fi
		return 0
	fi

	rm -f "$marker"
	git -C "$path" reset --quiet --hard "$new_revision"
	git -C "$path" clean --quiet -fd
	printf '%s\n' "$new_revision" > "$marker.next"
	mv "$marker.next" "$marker"
	printf 'synced %s at %s\n' "$name" "$new_revision"
}

while :; do
	sync_repository soha-contracts "$SOHA_CONTRACTS_REPOSITORY" "$SOHA_CONTRACTS_REF" || printf 'failed to sync soha-contracts; retrying\n' >&2
	sync_repository soha "$SOHA_REPOSITORY" "$SOHA_REF" || printf 'failed to sync soha; retrying\n' >&2
	sync_repository soha-web "$SOHA_WEB_REPOSITORY" "$SOHA_WEB_REF" || printf 'failed to sync soha-web; retrying\n' >&2
	sleep "$interval"
done
