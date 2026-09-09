# EC2 production deployment

The production API runs as Docker Compose on one EC2 instance in `eu-west-1`.
CloudFormation creates the host, retained encrypted data volume, API Gateway/Lambda HTTPS
proxy, Elastic IP, ECR repository, Redis secret, Systems Manager access, and GitHub OIDC
deployment role.

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
Manager or `/var/log/cloud-init-output.log` through a Session Manager shell.

New SES accounts begin in the sandbox. Verify the sender from Amazon's email and request
production access before sending reports to arbitrary recipients. A configuration set is
optional; leave `SesConfigurationSet` empty unless one already exists.

## Configure GitHub

Create a protected GitHub environment named `production`. Add the deployment role as
an environment secret and the remaining values as environment variables:

- Secret `AWS_DEPLOY_ROLE_ANR` from `GitHubDeployRoleArn`
- `EC2_INSTANCE_ID` from `InstanceId`
- `ECR_REPOSITORY_URI` from `EcrRepositoryUri`

Configure the repository variable `NEXT_PUBLIC_API_URL` from the stack's `ApiUrl` output
and enable GitHub Pages with GitHub Actions as its source. Adding a required reviewer to
the `production` environment makes API releases manual-approval deployments.

## First and subsequent releases

Run the `Deploy API to EC2` workflow manually after the GitHub variables and SES identity
are ready. Later merges to `main` deploy automatically when API-related paths change. The
workflow builds an AMD64 image, pushes it to ECR, and deploys its immutable digest through
Systems Manager.

The host health-checks Quart locally before the API Gateway/Lambda proxy exposes it. On
failure it restores the prior image reference and exits unsuccessfully. Application data
remains on the retained EBS volume.

## Recovery

Open a Session Manager shell without exposing SSH:

    aws ssm start-session --region eu-west-1 --target i-xxxxxxxx

Inspect the stack from `/opt/enceladus`:

    sudo docker compose ps
    sudo docker compose logs --tail 200 app

CloudFormation retains the data volume, Redis secret, and ECR repository if the stack is
deleted. Reattaching a retained volume to a replacement stack is a deliberate recovery
operation and is not automated.

The instance is deliberately pinned to launch-template version 1 so ordinary stack
updates cannot replace the host before its single-attach data volume is detached. Changes
to host bootstrap require an explicitly planned instance replacement; a fresh stack uses
the corrected version 1 from its newly created launch template.
