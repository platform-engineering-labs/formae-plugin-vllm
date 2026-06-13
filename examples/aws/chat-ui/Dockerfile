FROM python:3.12-slim
WORKDIR /app
COPY server.py .
ENV PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD python -c "import urllib.request;urllib.request.urlopen('http://localhost:8080/healthz')" || exit 1
CMD ["python", "server.py"]
