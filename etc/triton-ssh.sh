#!/bin/bash


script=$(triton-pssh -1 "$@")
echo $script
eval "$script"
