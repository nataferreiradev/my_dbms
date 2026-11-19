# Compila o binário numa imagem com Go e copia só ele para uma imagem pequena.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/my_dbms ./cmd/my_dbms

FROM alpine:3.20
RUN adduser -D -H dbms && mkdir -p /app/data && chown dbms /app/data
WORKDIR /app
COPY --from=build /out/my_dbms /usr/local/bin/my_dbms
COPY my_dbms.conf ./
USER dbms

# As tabelas ficam em /app/data (data_dir do my_dbms.conf).
VOLUME /app/data
EXPOSE 4321
ENTRYPOINT ["my_dbms"]
CMD ["-listen", "0.0.0.0:4321"]
