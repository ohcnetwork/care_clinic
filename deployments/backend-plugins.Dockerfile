# Adds a clinic's backend plugins to a published CARE backend image.
# The builder image is CARE's prod.Dockerfile "builder" stage for the same commit.
ARG BACKEND_BUILDER
ARG BACKEND_IMAGE

FROM ${BACKEND_BUILDER} AS plugins
ARG ADDITIONAL_PLUGS=""
ENV ADDITIONAL_PLUGS=$ADDITIONAL_PLUGS
RUN python3 /app/install_plugins.py

FROM ${BACKEND_IMAGE}
COPY --from=plugins --chown=django:django /app/.venv /app/.venv
