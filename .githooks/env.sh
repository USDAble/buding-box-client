#!/bin/sh
# OCTO-FORK: GUI Git clients do not inherit terminal tool paths.
for hook_tool_dir in "$HOME/.local/bin" /opt/homebrew/bin /usr/local/bin /usr/local/go/bin "$HOME/go/bin"; do
    if [ -d "$hook_tool_dir" ]; then
        PATH="${PATH:+$PATH:}$hook_tool_dir"
    fi
done
export PATH
unset hook_tool_dir

for hook_tool in make go node; do
    if ! command -v "$hook_tool" >/dev/null 2>&1; then
        echo "Error: Git hook cannot find $hook_tool. Install it or add its directory to the Git client's PATH." >&2
        exit 1
    fi
done
unset hook_tool
