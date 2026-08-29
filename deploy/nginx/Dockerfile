ARG NGINX_VERSION=1.25.5
FROM nginx:${NGINX_VERSION}-alpine AS builder

# Build deps for nginx-vod-module (ffmpeg for thumbnail, openssl for DRM, libxml2 for DFXP)
RUN apk add --no-cache \
    git \
    build-base \
    pcre-dev \
    zlib-dev \
    openssl-dev \
    ffmpeg-dev \
    libxml2-dev \
    curl

RUN git clone --depth 1 https://github.com/kaltura/nginx-vod-module.git /usr/src/nginx-vod-module

# Download nginx source matching the base image
RUN wget -O /tmp/nginx.tar.gz http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz \
    && tar -xzf /tmp/nginx.tar.gz -C /tmp

RUN cd /tmp/nginx-${NGINX_VERSION} \
    && ./configure \
        --with-compat \
        --add-dynamic-module=/usr/src/nginx-vod-module \
        --with-cc-opt="-O3" \
    && make modules

FROM nginx:${NGINX_VERSION}-alpine
# ffmpeg libs for vod thumbnail/volume_map (optional but needed for JIT)
RUN apk add --no-cache ffmpeg libxml2

COPY --from=builder /tmp/nginx-${NGINX_VERSION}/objs/ngx_http_vod_module.so /usr/lib/nginx/modules/ngx_http_vod_module.so
COPY nginx.conf /etc/nginx/nginx.conf.template

EXPOSE 80
CMD ["/bin/sh", "-c", "envsubst '$$INTERNAL_TOKEN' < /etc/nginx/nginx.conf.template > /etc/nginx/nginx.conf && exec nginx -g 'daemon off;'"]
