#!/bin/bash
set -a
source .env
set +a
envsubst < torrc.template > torrc
echo "torrc generated from template"