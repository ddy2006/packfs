ARG PREFIX
FROM ${PREFIX}postgres:18.3

    
ENV POSTGRES_DB=packfs \
    POSTGRES_USER=packfs \
    POSTGRES_PASSWORD=changeme

COPY initdb.d/[0-9]*.sql initdb.d/[0-9]*.sh /docker-entrypoint-initdb.d/
