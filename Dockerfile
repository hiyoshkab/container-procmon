# Build go app
FROM golang:1.26 AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /procmon ./cmd

# Python app to test procmon.
FROM python:3.13-slim
WORKDIR /app

COPY test-app/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY test-app/app.py .
COPY --from=build /procmon /usr/local/bin/procmon

ENV SAMPLE_INTERVAL=10s
ENV LOG_LEVEL=debug

EXPOSE 8080

CMD ["sh", "-c", "procmon & exec gunicorn --bind 0.0.0.0:8080 --workers 2 --timeout 0 app:app"]
