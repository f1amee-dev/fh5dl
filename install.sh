#!/bin/sh
# Install the latest fh5dl release on macOS or Linux:
#   curl -fsSL https://raw.githubusercontent.com/f1amee-dev/fh5dl/main/install.sh | sh
# Set FH5DL_DIR to choose the install folder, FH5DL_NO_OPEN=1 to skip opening
# a new terminal window afterwards.
set -eu

repo=f1amee-dev/fh5dl
case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) echo "fh5dl: unsupported OS $(uname -s); see https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) echo "fh5dl: unsupported CPU $(uname -m); see https://github.com/$repo/releases" >&2; exit 1 ;;
esac

if [ -n "${FH5DL_DIR:-}" ]; then
	dir=$FH5DL_DIR
elif [ -w /usr/local/bin ]; then
	dir=/usr/local/bin
else
	dir=$HOME/.local/bin
fi

asset=fh5dl_${os}_${arch}.tar.gz
url=https://github.com/$repo/releases/latest/download
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset..."
curl -fsSL "$url/$asset" -o "$tmp/$asset"
curl -fsSL "$url/checksums.txt" -o "$tmp/checksums.txt"
want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
	got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
	echo "fh5dl: checksum mismatch, not installing" >&2
	exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp" fh5dl
mkdir -p "$dir"
mv "$tmp/fh5dl" "$dir/fh5dl"
chmod +x "$dir/fh5dl"
echo "Installed $("$dir/fh5dl" --version) to $dir/fh5dl"

# Single-quote a value for any shell (sh, bash, zsh, fish).
q() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

# Put the folder on PATH for new terminals.
case ":$PATH:" in
*":$dir:"*) ;;
*)
	case "${SHELL:-}" in
	*zsh) rc=$HOME/.zshrc line="export PATH=$(q "$dir"):\"\$PATH\"" ;;
	*bash)
		rc=$HOME/.bashrc line="export PATH=$(q "$dir"):\"\$PATH\""
		# macOS Terminal starts bash as a login shell, which skips .bashrc and
		# reads only the first of these that exists.
		if [ "$os" = darwin ]; then
			rc=$HOME/.bash_profile
			for f in .bash_profile .bash_login .profile; do
				[ -f "$HOME/$f" ] && rc=$HOME/$f && break
			done
		fi
		;;
	*fish)
		rc=$HOME/.config/fish/conf.d/fh5dl.fish line="set -gx PATH $(q "$dir") \$PATH"
		mkdir -p "$(dirname "$rc")"
		;;
	*) rc=$HOME/.profile line="export PATH=$(q "$dir"):\"\$PATH\"" ;;
	esac
	grep -qsF "$line" "$rc" || printf '\n# fh5dl\n%s\n' "$line" >>"$rc"
	echo "Added $dir to PATH in $rc"
	;;
esac

# Open a fresh terminal in Downloads with fh5dl ready, unless this is CI, SSH
# or opted out. The commands work in sh, bash, zsh and fish.
downloads=$HOME/Downloads
[ "$os" = linux ] && downloads=$(xdg-user-dir DOWNLOAD 2>/dev/null || echo "$downloads")
welcome="cd $(q "$downloads"); clear; $(q "$dir/fh5dl") --help; echo; echo 'fh5dl is ready. Type fh5dl and a space, paste a link, press Enter.'"
if [ -n "${CI:-}${SSH_CONNECTION:-}${FH5DL_NO_OPEN:-}" ]; then
	echo "Open a new terminal and run: fh5dl"
elif [ "$os" = darwin ]; then
	# AppleScript string: escape backslashes and double quotes.
	script=$(printf '%s' "$welcome" | sed 's/\\/\\\\/g; s/"/\\"/g')
	osascript -e "tell application \"Terminal\" to do script \"$script\"" \
		-e 'tell application "Terminal" to activate' >/dev/null
	echo "Opened a new Terminal window with fh5dl ready."
elif [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] && command -v x-terminal-emulator >/dev/null 2>&1; then
	x-terminal-emulator -e sh -c "$welcome; exec \"\${SHELL:-sh}\"" >/dev/null 2>&1 &
	echo "Opened a new terminal window with fh5dl ready."
else
	echo "Open a new terminal and run: fh5dl"
fi
