#!/bin/sh

echo "Welcome to commitlog 0.0.1"

# workaround error "raft: Failed to commit logs: EOF ... log.*index.Write: EOF"
rm -rf /var/run/commitlog/data/log /var/run/commitlog/data/raft

/bin/commitlog "$@"