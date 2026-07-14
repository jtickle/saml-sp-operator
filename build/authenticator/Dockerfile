# Encapsulated Shibboleth SP "authenticator" for the forward-auth spike.
#
# Roles inside this one image:
#   shibd            - the SAML brain (session cache, assertion processing)
#   shibauthorizer   - FastCGI authorizer  -> answers the /authcheck subrequest
#   shibresponder    - FastCGI responder   -> serves /Shibboleth.sso/* (ACS, Login, Logout, Metadata)
#   nginx            - FastCGI -> HTTP adapter; exposes the above over plain HTTP
#   supervisord      - runs all four in one pod
#
# Debian is deliberate: shibboleth-sp-utils ships the FastCGI binaries here.
FROM debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        shibboleth-sp-utils \
        nginx-light \
        supervisor \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

# nginx (www-data) must be able to read the FastCGI sockets owned by _shibd,
# and _shibd must own the socket/runtime dir.
RUN usermod -aG _shibd www-data \
    && mkdir -p /run/shibboleth \
    && chown _shibd:_shibd /run/shibboleth

# Only the rarely-changing bits are baked in. shibboleth2.xml and nginx.conf
# come from a ConfigMap so you can iterate without rebuilding (see k8s/10-*).
COPY supervisord.conf /etc/supervisor/conf.d/shib-spike.conf

EXPOSE 8080
CMD ["/usr/bin/supervisord", "-n", "-c", "/etc/supervisor/supervisord.conf"]
