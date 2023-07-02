FROM quay.io/argoproj/argo-rollouts:v1.5.1

COPY ./dist/plugin-linux-amd64 /home/argo-rollouts/plugin-linux-amd64

ENTRYPOINT ["/bin/rollouts-controller"]