FROM python:3.14-slim AS deps
WORKDIR /app
COPY pyproject.toml ./
COPY app ./app
RUN pip install --no-cache-dir .
RUN python -c "from chromadb.utils.embedding_functions import DefaultEmbeddingFunction; DefaultEmbeddingFunction()(['warm'])"

FROM python:3.14-slim
RUN useradd --create-home orchestrator
WORKDIR /app
COPY --from=deps /usr/local/lib/python3.14/site-packages /usr/local/lib/python3.14/site-packages
COPY --from=deps /usr/local/bin/uvicorn /usr/local/bin/uvicorn
COPY --from=deps /root/.cache/chroma /home/orchestrator/.cache/chroma
COPY app ./app
ENV HOME=/home/orchestrator
ENV PYTHONUNBUFFERED=1
USER orchestrator
EXPOSE 8000
CMD ["uvicorn", "app.main:create_app", "--factory", "--host", "0.0.0.0", "--port", "8000"]
