FROM node:24-alpine AS build

WORKDIR /workspace/admin

# Copying the bounded admin workspace keeps all three tracked templates in the
# image context. The ignored local selector is excluded by .dockerignore;
# ADMIN_UI is the explicit CI/deploy choice. Only the selected package closure is
# installed, while the single workspace lockfile remains authoritative. The
# resolver still reads legacy .ui-profile.json and reports UI_PROFILE_MISMATCH
# when a concrete deployment choice conflicts with that compatibility profile.
COPY admin/ ./
ARG ADMIN_UI=""
ARG NPM_REGISTRY=https://registry.npmjs.org
RUN --mount=type=cache,id=gin-vben-corepack,target=/root/.cache/node/corepack \
    --mount=type=cache,id=gin-vben-pnpm,target=/pnpm/store \
    corepack enable \
    && pnpm config set store-dir /pnpm/store \
    && pnpm config set registry "${NPM_REGISTRY}" --location=project \
    && pnpm config set fetch-timeout 600000 --location=project \
    && UI_PACKAGE="$(node ./scripts/docker-build-ui.mjs --ui "${ADMIN_UI}" --print-package)" \
    && pnpm install --frozen-lockfile --filter "${UI_PACKAGE}..." --ignore-scripts \
    && pnpm -r run --if-present stub \
    && node ./scripts/docker-build-ui.mjs --ui "${ADMIN_UI}" --out /out

FROM nginx:1.27-alpine
COPY admin/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out/ /usr/share/nginx/html/
EXPOSE 80
