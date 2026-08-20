# Build one of the management UI templates.
FROM node:22-alpine AS build

WORKDIR /workspace
RUN corepack enable

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps ./apps
COPY packages ./packages
COPY internal ./internal
COPY scripts ./scripts

RUN pnpm install --frozen-lockfile --ignore-scripts \
    && pnpm -r run --if-present stub

ARG UI_APP=web-antd
RUN case "$UI_APP" in \
      web-antd|web-ele|web-naive) ;; \
      *) echo "Unsupported UI_APP=$UI_APP" >&2; exit 2 ;; \
    esac \
    && pnpm run "build:${UI_APP#web-}" \
    && mkdir -p /out \
    && cp -R "apps/${UI_APP}/dist/." /out/

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out/ /usr/share/nginx/html/
EXPOSE 80
