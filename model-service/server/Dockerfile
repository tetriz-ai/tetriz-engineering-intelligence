# One image runs either server; docker-compose sets the command per service.
FROM python:3.13-slim
WORKDIR /app
RUN pip install --no-cache-dir -U pip
# Install the CPU-only torch build first so sentence-transformers does not pull
# the ~1 GB CUDA wheels — useless in this CPU-only service, on any architecture.
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu
COPY server/requirements.txt /app/server/requirements.txt
RUN pip install --no-cache-dir -r server/requirements.txt
# the SDK + frozen contract (the bundle is mounted at runtime, not baked)
COPY contract/ /app/contract/
COPY sdk/ /app/sdk/
COPY stage/ /app/stage/
COPY server/ /app/server/
ENV PYTHONPATH=/app
# Run as an unprivileged user, not root.
RUN useradd --create-home --uid 10001 app && chown -R app:app /app
USER app
EXPOSE 8000 8080
# default; overridden per service in docker-compose.yml
CMD ["uvicorn", "server.model_server:app", "--host", "0.0.0.0", "--port", "8000"]
