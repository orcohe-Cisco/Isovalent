#!/usr/bin/env bash
# macOS double-click launcher. Double-click this file in Finder to install
# isovalent-control in demo mode against your current kubectl context.
# (Right-click → Open the first time to bypass Gatekeeper.)
cd "$(dirname "$0")" || exit 1
echo "Installing isovalent-control (demo mode) against your current kube-context…"
echo
./install.sh "$@"
echo
echo "Press any key to close."
read -r -n 1 _
