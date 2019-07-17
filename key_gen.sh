#!/bin/bash

openssl genrsa -out ./frontend/cord/config/keys/private_key 2048
openssl rsa -pubout -in ./frontend/cord/config/keys/private_key -out ./frontend/cord/config/keys/public_key.pub
