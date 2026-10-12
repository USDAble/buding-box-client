#!/bin/sh
# OCTO-FORK: GUI Git on Windows may not inherit GNU Make's installation path.
# Keep the existing Makefile as the only owner of the check sequence.
# Git GUI may put System32 before Git's POSIX tools (notably find.exe).
# Pin Make to this same shell and tool directory, without changing global PATH.
if command -v cygpath >/dev/null 2>&1; then
    git_tools="$(dirname "$(command -v cygpath)")"
    PATH="$git_tools:$PATH"
    MAKESHELL="$(cygpath -m "$git_tools/sh.exe")"
    export PATH MAKESHELL
    # Discover the installed C toolchain in already-running Git GUI processes.
    for compiler_bin in "$(cygpath -u "$LOCALAPPDATA")"/Microsoft/WinGet/Packages/BrechtSanders.WinLibs.POSIX.UCRT_*/mingw64/bin; do
        if [ -x "$compiler_bin/gcc.exe" ]; then
            PATH="$compiler_bin:$PATH"
            export PATH
            break
        fi
    done
fi
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
