# EC2 production deployment

The production API runs natively from a [Nix](https://nixos.org) flake on one EC2
instance in `eu-west-1`. CloudFormation creates the host, retained encrypted data
volume, API Gateway/Lambda HTTPS proxy, Elastic IP, Redis secret, Systems Manager
access, and GitHub OIDC deployment role. The host installs Nix at boot, builds the
[`api`](../flake.nix) and `population` outputs, and manages the services with
systemd units (`enceladus-api`, `enceladus-redis`, `enceladus-population`). There is
no Docker, container registry, or SSH.

## Prerequisites

- An AWS account with a VPC and public subnet in `eu-west-1`.
- A verified Amazon SES identity in `eu-west-1`.
- This repository pushed to GitHub before bootstrapping the stack.

## Provision infrastructure

Validate and deploy from an authenticated operator workstation:

    aws cloudformation validate-template \
      --region eu-west-1 \
      --template-body file://infra/ec2.yml

    aws cloudformation deploy \
      --region eu-west-1 \
      --stack-name enceladus-production \
      --template-file infra/ec2.yml \
      --capabilities CAPABILITY_IAM \
      --parameter-overrides \
        VpcId=vpc-xxxxxxxx \
        PublicSubnetId=subnet-xxxxxxxx \
        CorsOrigin=https://rodrinac.github.io \
        SesSender=jose.rs.inacio@gmail.com

If the account already has the GitHub Actions OIDC provider, also pass its ARN as
`GitHubOidcProviderArn`.

The stack intentionally has no SSH ingress. Inspect bootstrap progress with Systems
Manager or `/var/log/cloud-init-output.log` through a Session Manager shell. The
first boot installs Nix, clones the repository to `/opt/enceladus`, runs
[`deploy/bootstrap.sh`](bootstrap.sh) and builds the release closure from the local
flake before starting the services.

New SES accounts begin in the sandbox. Verify the sender from Amazon's email and request
production access before sending reports to arbitrary recipients. A configuration set is
optional; leave `SesConfigurationSet` empty unless one already exists.

## Configure GitHub

Create a protected GitHub environment named `production`. Add the deployment role as
an environment secret and the remaining value as an environment variable:

- Secret `AWS_DEPLOY_ROLE_ANR` from `GitHubDeployRoleArn`
- `EC2_INSTANCE_ID` from `InstanceId`

Configure the repository variable `NEXT_PUBLIC_API_URL` from the stack's `ApiUrl` output
and enable GitHub Pages with GitHub Actions as its source. Adding a required reviewer to
the `production` environment makes API releases manual-approval deployments.

## First and subsequent releases

Run the `Deploy API to EC2` workflow manually after the GitHub variables and SES identity
are ready. Later merges to `main` deploy automatically when API-related paths change. The
workflow invokes `deploy/deploy.sh` on the host through Systems Manager; the script
refreshes the `/opt/enceladus` checkout, rebuilds the flake outputs, feeds the runtime
environment (including the Redis password from Secrets Manager) to the systemd units and
health-checks the API before promoting the release.

The host health-checks the API locally before the API Gateway/Lambda proxy exposes it. On
failure the previous build is restored (the earlier `current-*.previous` out-links) and
the script exits unsuccessfully. Application data remains on the retained EBS volume.

## Migrate an existing Docker-era host

If the stack was created before this repo dropped Docker, the instance still runs the
legacy Compose user data and the first `Deploy API to EC2` run fails fast because
`/etc/enceladus/runtime.env` does not exist. Keep the same instance and migrate it in
place: run `deploy/bootstrap.sh` on the host once through Systems Manager. It adopts the
values CloudFormation originally injected (`/opt/enceladus/runtime.env`), mounts the data
volume, installs Nix, stops and disables the legacy Docker stack (moving any old PDFs from
`/srv/enceladus/reports` into `/srv/enceladus/relatorios`), writes the systemd units and
performs the initial Nix deploy:

    aws ssm send-command \
      --region eu-west-1 \
      --document-name AWS-RunShellScript \
      --instance-ids i-xxxxxxxx \
      --parameters 'commands=["sudo curl --fail --location --retry 5 https://raw.githubusercontent.com/rodrinac/enceladus/main/deploy/bootstrap.sh --output /opt/enceladus/deploy/bootstrap.sh && sudo bash /opt/enceladus/deploy/bootstrap.sh"]' \
      --query Command.CommandId --output text

Watch it with `aws ssm get-command-invocation --command-id <id> --instance-id i-xxxxxxxx`.
A fresh stack created from the current template does not need this step: its user data
runs the same bootstrap automatically.

## Recovery

Open a Session Manager shell without exposing SSH:

    aws ssm start-session --region eu-west-1 --target i-xxxxxxxx

Inspect the stack:

    sudo systemctl status enceladus-api enceladus-redis enceladus-population
    sudo journalctl --unit enceladus-api --since "-2 hours"
    sudo ls -l /opt/enceladus/current-* | head

The repository and Nix store live in `/opt/enceladus` and `/nix`; the EBS volume under
`/srv/enceladus` holds reports, Redis AOF and the IBGE/DataSUS cache. Rebuild and
redeploy manually from the host:

    sudo bash /opt/enceladus/deploy/deploy.sh main

CloudFormation retains the data volume and Redis secret if the stack is deleted.
Reattaching a retained volume to a replacement stack is a deliberate recovery operation
and is not automated.

The instance is deliberately pinned to launch-template version 1 so ordinary stack
updates cannot replace the host before its single-attach data volume is detached. Changes
to host bootstrap require an explicitly planned instance replacement; a fresh stack uses
the corrected version 1 from its newly created launch template.