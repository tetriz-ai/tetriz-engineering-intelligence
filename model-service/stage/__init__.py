# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""AIDLC stage classification: which lifecycle stage each AI-harness artifact serves.

``prompt`` holds the prompt, reply schema and bucket -> stage mapping; ``classify`` runs the
batched, composition-varied, strict-majority vote; ``redact`` scrubs what is sent. Served by
``server/model_server.py`` at ``POST /classify-stage``. Nothing here imports the model library.
"""
