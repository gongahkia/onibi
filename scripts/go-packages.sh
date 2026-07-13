#!/usr/bin/env sh
set -eu

go list ./... | awk '!/\/frontend\/node_modules\//'
