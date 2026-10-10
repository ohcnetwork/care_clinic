# Rebuilds the CARE frontend bundle when a clinic changes a setting that is
# compiled in. Settings applied at start stay as placeholders (clinic.env).
ARG FRONTEND_BUILDER
ARG FRONTEND_IMAGE

FROM ${FRONTEND_BUILDER} AS build
COPY clinic.env /app/.env.local
RUN npm run build

FROM ${FRONTEND_IMAGE}
RUN rm -rf /care-clinic/html
COPY --from=build /app/build /care-clinic/html
