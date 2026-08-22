FROM node:22-alpine AS build

WORKDIR /workspace
RUN corepack enable
COPY admin/package.json admin/pnpm-workspace.yaml admin/pnpm-lock.yaml ./
COPY admin/apps ./apps
COPY admin/packages ./packages
COPY admin/internal ./internal
COPY admin/scripts ./scripts
ARG UI_APP=web-antd
RUN pnpm install --frozen-lockfile --ignore-scripts \
    && pnpm run build --filter=@vben/${UI_APP} \
    && mkdir -p /out \
    && cp -R apps/${UI_APP}/dist/. /out/

FROM nginx:1.27-alpine
COPY admin/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out/ /usr/share/nginx/html/
EXPOSE 80
