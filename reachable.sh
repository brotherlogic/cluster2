#!/bin/bash

[ ! -e ~/.ssh ] && ssh-keygen

for host in $(grep 192 inventory/my-cluster/hosts.ini); do ssh-copy-id $host; done
