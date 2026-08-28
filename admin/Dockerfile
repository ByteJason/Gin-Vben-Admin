# Build one of the management UI templates from the shared workspace without
# removing the other tracked templates.
FROM node:24-alpine AS build

WORKDIR /workspace
RUN corepack enable

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps ./apps
COPY packages ./packages
COPY internal ./internal
COPY scripts ./scripts

ARG ADMIN_UI=""
RUN UI_PACKAGE="$(node ./scripts/docker-build-ui.mjs --ui "${ADMIN_UI}" --print-package)" \
    && pnpm install --frozen-lockfile --filter "${UI_PACKAGE}..." --ignore-scripts \
    && pnpm -r run --if-present stub \
    && node ./scripts/docker-build-ui.mjs --ui "${ADMIN_UI}" --out /out

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out/ /usr/share/nginx/html/
EXPOSE 80
