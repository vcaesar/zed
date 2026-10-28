#!/usr/bin/env bash
#
# Installs ZedSearch the quick way, for adventurers that want to spend more time
# grooming their cats.
#
# Requires curl, grep, cut, tar, uname, chmod, mv, rm.

[[ $- = *i* ]] && echo "Don't source this script!" && return 10

header() {
    cat 1>&2 <<EOF
ZedSearch Installer

Website: https://github.com/vcaesar/zedsearch
Docs: https://zincsearch-docs.zinc.dev
Repo: https://github.com/vcaesar/zedsearch

EOF
}

check_cmd() {
	command -v "$1" > /dev/null 2>&1
}

check_tools() {
	Tools=("curl" "grep" "cut" "tar" "uname" "chmod" "mv" "rm")

	for tool in ${Tools[*]}; do
		if ! check_cmd $tool; then
			echo "Aborted, missing $tool, sorry!"
			exit 6
		fi
	done
}

install_zedsearch()
{
	trap 'echo -e "Aborted, error $? in command: $BASH_COMMAND"; trap ERR; exit 1' ERR
	install_path="/usr/local/bin"
	zed_os="unsupported"
	zed_arch="unknown"
	zed_arm=""

	header
	check_tools

	if [[ -n "$PREFIX" ]]; then
		install_path="$PREFIX/bin"
	fi

	# Fall back to /usr/bin if necessary
	if [[ ! -d $install_path ]]; then
		install_path="/usr/bin"
	fi

	# Not every platform has or needs sudo (https://termux.com/linux.html)
	((EUID)) && sudo_cmd="sudo"

	#########################
	# Which OS and version? #
	#########################

	zed_bin="zedsearch"
	zed_dl_ext=".tar.gz"

	# NOTE: `uname -m` is more accurate and universal than `arch`
	# See https://en.wikipedia.org/wiki/Uname
	unamem="$(uname -m)"
	if [[ $unamem == *aarch64* ]]; then
		zed_arch="arm64"
    elif [[ $unamem == *arm64* ]]; then
		zed_arch="arm64"
	elif [[ $unamem == *64* ]]; then
		zed_arch="x86_64"
	elif [[ $unamem == *armv6l* ]]; then
		zed_arch="arm"
		zed_arm="v6"
	elif [[ $unamem == *armv7l* ]]; then
		zed_arch="arm"
		zed_arm="v7"
	else
		echo "Aborted, unsupported or unknown architecture: $unamem"
		return 2
	fi

	unameu="$(tr '[:lower:]' '[:upper:]' <<<$(uname))"
	if [[ $unameu == *DARWIN* ]]; then
		zed_os="Darwin"
		version=${vers##*ProductVersion:}
	elif [[ $unameu == *LINUX* ]]; then
		zed_os="Linux"
	elif [[ $unameu == *FREEBSD* ]]; then
		zed_os="freebsd"
	elif [[ $unameu == *OPENBSD* ]]; then
		zed_os="openbsd"
	elif [[ $unameu == *WIN* || $unameu == MSYS* ]]; then
		# Should catch cygwin
		sudo_cmd=""
		zed_os="Windows"
		zed_bin=$zed_bin.exe
	else
		echo "Aborted, unsupported or unknown os: $uname"
		return 6
	fi

	########################
	# Download and extract #
	########################

	echo "Downloading ZedSearch for ${zed_os}/${zed_arch}${zed_arm}..."
	zed_file="zedsearch_${zed_os}_${zed_arch}${zed_arm}${zed_dl_ext}"

	if [[ "$#" -eq 0 ]]; then
		# get latest release
		zed_tag=$(curl -s https://api.github.com/repos/vcaesar/zedsearch/releases/latest | grep 'tag_name' | cut -d\" -f4)
		zed_version=$(echo ${zed_tag} | cut -c2-)
	elif [[ "$#" -gt 1 ]]; then
		echo "Too many arguments."
		exit 1
	elif [ -n $1  ]; then
		# try to get passed version
		zed_tag="v$1"
		zed_version=$1
	fi

	zed_url="https://github.com/vcaesar/zedsearch/releases/download/${zed_tag}/zedsearch_${zed_version}_${zed_os}_${zed_arch}${zed_arm}.tar.gz"

	dl="/tmp/$zed_file"
	rm -rf -- "$dl"

    echo "Downloading $zed_url"

	curl -fsSL "$zed_url" -o "$dl"

	echo "Extracting..."
	case "$zed_file" in
		*.tar.gz) tar -xzf "$dl" -C "$PREFIX/tmp/" "$zed_bin" ;;
	esac
	chmod +x "$PREFIX/tmp/$zed_bin"

	echo "Putting ZedSearch in $install_path (may require password)"
	$sudo_cmd mv "$PREFIX/tmp/$zed_bin" "$install_path/$zed_bin"
	$sudo_cmd rm -- "$dl"

	# check installation
	# $zed_bin -version

	echo "Successfully installed ZedSearch"
	trap ERR
	return 0
}

install_zedsearch $@
