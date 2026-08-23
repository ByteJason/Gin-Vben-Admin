FROM node:24-alpine AS build

WORKDIR /workspace/admin

# Copying the bounded admin workspace makes tracked admin/.ui-profile.json
# available when present. ADMIN_UI remains the explicit pristine-CI seam; the
# resolver exits with UI_PROFILE_MISMATCH when both authorities disagree.
COPY admin/ ./
ARG ADMIN_UI=""
ARG NPM_REGISTRY=https://registry.npmjs.org
RUN --mount=type=cache,id=gin-vben-corepack,target=/root/.cache/node/corepack \
    --mount=type=cache,id=gin-vben-pnpm,target=/pnpm/store \
    corepack enable \
    && pnpm config set store-dir /pnpm/store \
    && pnpm config set registry "${NPM_REGISTRY}" --location=project \
    && pnpm config set fetch-timeout 600000 --location=project \
    && pnpm install --frozen-lockfile --ignore-scripts \
    && pnpm -r run --if-present stub \
    && node ./scripts/docker-build-ui.mjs --ui "${ADMIN_UI}" --out /out

FROM nginx:1.27-alpine
COPY admin/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out/ /usr/share/nginx/html/
EXPOSE 80
