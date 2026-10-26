ARG PREFIX
FROM ${PREFIX}postgres:18.3

    
ENV POSTGRES_DB=packfs \
    POSTGRES_USER=packfs \
    POSTGRES_PASSWORD=changeme

COPY arcset_schema.sql /docker-entrypoint-initdb.d/00-arcset.sql
