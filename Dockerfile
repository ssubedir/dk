FROM node:22-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
# The bundled site calls /api on the same origin served by Go.
RUN VITE_API_BASE_URL= npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=backend /out/server /server
COPY --from=frontend /src/dist /srv/frontend
USER 65532:65532
ENV PORT=10000
ENV FRONTEND_DIST=/srv/frontend
EXPOSE 10000
ENTRYPOINT ["/server"]
