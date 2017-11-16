#!/bin/bash

script=$(triton-pssh -1 "$@")
eval "$script"
