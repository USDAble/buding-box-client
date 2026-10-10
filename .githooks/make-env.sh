#!/bin/sh
# OCTO-FORK: GUI Git on Windows may not inherit GNU Make's installation path.
# Keep the existing Makefile as the only owner of the check sequence.
if ! command -v make >/dev/null 2>&1 && command -v cygpath >/dev/null 2>&1; then
    for program_files in "$(printenv 'ProgramFiles(x86)')" "$PROGRAMFILES"; do
        [ -n "$program_files" ] || continue
        make_bin="$(cygpath -u "$program_files")/GnuWin32/bin"
        if [ -x "$make_bin/make.exe" ]; then
            PATH="$make_bin:$PATH"
            export PATH
            break
        fi
    done
fi

if ! command -v make >/dev/null 2>&1; then
    echo "Error: GNU Make is required. On Windows: winget install --id GnuWin32.Make --exact"
    exit 1
fi
