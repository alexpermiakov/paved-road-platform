# IDP Architecture - Multi-Account, Multi-Cluster, Multi-Region

## Overview

This IDP uses a **distributed ArgoCD architecture** with one ArgoCD instance per cluster, deployed across three AWS accounts with **multi-region disaster recovery**.

---

## Setup

### 1. App Team onboarding

For app teams to deploy their services, see the complete guide:

- **[CI Workflow Template](templates/app-team-ci-workflow.yaml)** - GitHub Actions workflow

**Quick Summary:** App teams set up a GitHub Actions workflow that automatically opens PRs to this platform repo when they tag releases. Platform team reviews and merges PRs to trigger deployments.

---

### 2. Apply Service Control Policies (Management Account Only)

**Do this once in the management account** to enforce organization-wide security guardrails.

SCPs set the maximum permissions for all accounts - even admins can't bypass them. This is required for PCI-DSS, HIPAA, and SOC2 compliance.

| Policy File                                      | Purpose                                          |
| ------------------------------------------------ | ------------------------------------------------ |
| `deny-security-service-disruption.json`          | Prevents disabling GuardDuty, CloudTrail, Config |
| `deny-root-account-actions.json`                 | Blocks root user, requires MFA for IAM changes   |
| `require-encryption-and-deny-public-access.json` | Enforces encryption, blocks public RDS/S3        |
| `protect-critical-resources.json`                | Protects KMS keys, Flow Logs, Backup vaults      |

#### Apply via AWS Console

```bash
# 1. Log in to the management account
export AWS_PROFILE=management  # or your management account profile

# 2. Verify you're in the management account
aws organizations describe-organization --query 'Organization.MasterAccountId' --output text
```

Then in the AWS Console:

1. Go to **AWS Organizations → Policies → Service control policies**
2. Enable SCPs if not already enabled
3. For each policy in `policies/scp/`:
   - Click **Create policy**
   - Paste the JSON content
   - Name it (e.g., `deny-security-service-disruption`)
   - Attach to the root OU or specific OUs (dev, staging, prod)

> 📖 See [policies/scp/README.md](policies/scp/README.md) for detailed policy descriptions.

---

### 3. AWS Account Bootstrap for GitHub Actions

**Do this once per AWS account** to enable GitHub Actions to deploy infrastructure via Terraform.

#### Prerequisites

- AWS account with admin access
- AWS CLI installed locally
- Your GitHub repository: `YOUR-ORG/idp`

#### Step 1: Create GitHub OIDC Provider

```bash
# Set AWS profile for the target account (from ~/.aws/credentials or ~/.aws/config)
# Set AWS_PROFILE
export AWS_PROFILE=<your-profile-name>

# Get your AWS account ID
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
echo "Setting up OIDC in account: $AWS_ACCOUNT_ID"

# Create OIDC provider for GitHub Actions
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --tags Key=Name,Value=GitHubActionsOIDC
```

#### Step 2: Create GitHubActionsRole

**Policy Files** (in `policies/iam/`):

| File                                        | Purpose                                              | Use In             |
| ------------------------------------------- | ---------------------------------------------------- | ------------------ |
| `github-actions-trust-policy.json`          | Trust policy (who can assume the role)               | All accounts       |
| `github-actions-policy-entry-infra.json`    | IAM permissions for EKS, ECR, VPC, AutoScaling       | Dev, Staging, Prod |
| `github-actions-policy-entry-platform.json` | IAM permissions for IAM, KMS, Logs, S3, Secrets, SSM | Dev, Staging, Prod |
| `github-actions-policy-tooling.json`        | IAM permissions for ECR                              | Tooling only       |

> The entry permissions are split across two policies because a single managed policy cannot exceed AWS's 6,144-character limit. Attach **both** to the role on Dev/Staging/Prod accounts.

**For Dev/Staging/Prod accounts** (EKS clusters):

```bash
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# 1. Prepare trust policy (substitutes account ID)
sed "s/\${AWS_ACCOUNT_ID}/$AWS_ACCOUNT_ID/g" policies/iam/github-actions-trust-policy.json > /tmp/trust-policy.json

# 2. Create the two IAM permission policies (split to stay under the 6,144-char limit)
aws iam create-policy \
  --policy-name GitHubActionsInfraPolicy \
  --policy-document file://policies/iam/github-actions-policy-entry-infra.json

aws iam create-policy \
  --policy-name GitHubActionsPlatformPolicy \
  --policy-document file://policies/iam/github-actions-policy-entry-platform.json

# 3. Create role with trust policy, then attach both permission policies
aws iam create-role \
  --role-name GitHubActionsRole \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --description "Role for GitHub Actions to manage EKS infrastructure" \
  --max-session-duration 7200

aws iam attach-role-policy \
  --role-name GitHubActionsRole \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/GitHubActionsInfraPolicy

aws iam attach-role-policy \
  --role-name GitHubActionsRole \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/GitHubActionsPlatformPolicy

rm /tmp/trust-policy.json

```

**For Tooling account** (ECR registry):

```bash
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# 1. Prepare trust policy (substitutes account ID)
sed "s/\${AWS_ACCOUNT_ID}/$AWS_ACCOUNT_ID/g" policies/iam/github-actions-trust-policy.json > /tmp/trust-policy.json

# 2. Create IAM policy for ECR permissions
aws iam create-policy \
  --policy-name GitHubActionsIDPPolicy \
  --policy-document file://policies/iam/github-actions-policy-tooling.json

# 3. Create role with trust policy, then attach permissions
aws iam create-role \
  --role-name GitHubActionsRole \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --description "Role for GitHub Actions to manage ECR"

aws iam attach-role-policy \
  --role-name GitHubActionsRole \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/GitHubActionsIDPPolicy

rm /tmp/trust-policy.json
```

#### Step 3: Create S3 Bucket for Terraform State

```bash
# Create S3 bucket for Terraform state (one-time per account)
# Replace with your naming convention, e.g., terraform-state-{company}-{env}
BUCKET_NAME="terraform-state-<your-prefix>"  # e.g., terraform-state-acme-dev

aws s3api create-bucket \
  --bucket $BUCKET_NAME \
  --region us-west-2 \
  --create-bucket-configuration LocationConstraint=us-west-2

# Enable versioning
aws s3api put-bucket-versioning \
  --bucket $BUCKET_NAME \
  --versioning-configuration Status=Enabled

echo "S3 bucket created: $BUCKET_NAME"
```

---

## Usage

### Get Your SSO Role ARN for GitHub Actions

To allow console access to EKS clusters, you need to configure your SSO role in the GitHub Actions workflows. Run this in each AWS account after logging in via SSO:

```bash
# Log in via SSO (visit https://SSO_ID.awsapps.com/start)
export AWS_PROFILE=<your-profile-name>  # or skip if already set

# Get your role ARN (extracts the full path including aws-reserved/sso.amazonaws.com/...)
IDENTITY=$(aws sts get-caller-identity --query Arn --output text)
ROLE_NAME=$(echo "$IDENTITY" | sed 's|.*:assumed-role/\([^/]*\)/.*|\1|')
aws iam get-role --role-name "$ROLE_NAME" --query 'Role.Arn' --output text
```

Copy the output and add it to the GitHub environment secret:

1. Go to **Settings → Environments** in your GitHub repo
2. Create environments: `dev`, `staging`, `prod` (if not already created)
3. In each environment, add secret `ADMIN_ROLE_ARNS` with the SSO role ARN for that account

### Connect to EKS Cluster

After deployment:

```bash
# Get your SSO portal URL from: Main Account → IAM Identity Center → AWS access portal URL
# Visit https://<your-sso-id>.awsapps.com/start, click "Access keys", and copy the credentials
export AWS_PROFILE=<your-profile-name>  # or skip if already set
# export AWS_PROFILE=<account-id>_AdministratorAccess

# Verify AWS access
aws sts get-caller-identity

# Update kubeconfig - replace <PR> with your PR number
aws eks update-kubeconfig --region us-west-2 --name k8s-pr-<PR>

# Verify connection
kubectl get nodes
```

### Update GitHub App Secret

After the first `terraform apply` creates the placeholder secret, update it with actual credentials:

```bash
# Set your values from GitHub App settings page
export GITHUB_APP_ID=  # From "App ID" field
export GITHUB_APP_INSTALLATION_ID=  # From installation URL
export GITHUB_APP_PEM_FILE=  # Path to downloaded .pem file
export PR_NUMBER=  # Your PR number (dev), or 0 (staging/prod)

# Find secret by PR number
SECRET_ARN=$(aws secretsmanager list-secrets --region us-west-2 \
  --query "SecretList[?contains(Name, 'argocd/github-app-pr-${PR_NUMBER}')].ARN | [0]" \
  --output text)

echo "Found: $SECRET_ARN"

aws secretsmanager put-secret-value \
  --region us-west-2 \
  --secret-id "$SECRET_ARN" \
  --secret-string "$(jq -n \
    --arg appID "$GITHUB_APP_ID" \
    --arg installationID "$GITHUB_APP_INSTALLATION_ID" \
    --rawfile privateKey "$GITHUB_APP_PEM_FILE" \
    '{appID: $appID, installationID: $installationID, privateKey: $privateKey}')"
```

> For GitHub App setup instructions, see [GitHub App Setup for ArgoCD](#github-app-setup-for-argocd) below.

### Day-2 Operations

Once the cluster is running, see **[docs/operations.md](docs/operations.md)** for
recurring operational tasks — accessing the ArgoCD, Hubble, Argo Rollouts, and
Grafana dashboards, verifying WireGuard node encryption, checking cluster resource
usage, monitoring Karpenter and Kubecost, and upgrading Kubernetes.

### GitHub App Setup for ArgoCD

ArgoCD needs access to pull configurations from this repo. We use a **GitHub App** instead of a PAT for better security:

- ✅ Not tied to a personal account
- ✅ Scoped to specific repositories
- ✅ Auto-refreshing tokens (for staging/prod)

#### Step 1: Create the GitHub App

1. Go to **GitHub → Settings → Developer settings → GitHub Apps → New GitHub App**
2. Configure:

| Setting                              | Value                                       |
| ------------------------------------ | ------------------------------------------- |
| **App name**                         | `idp-argocd` (or your preferred name)       |
| **Homepage URL**                     | `https://github.com/YOUR-ORG/idp` (any URL) |
| **Webhook Active**                   | ❌ Unchecked                                |
| **Repository permissions**           | Contents: **Read-only**                     |
| **Where can this app be installed?** | Only on this account                        |

3. Click **Create GitHub App**
4. Note the **App ID** from the app settings page
5. Scroll down and click **Generate a private key** → downloads a `.pem` file
6. Click **Install App** → select your org → choose "Only select repositories" → select `idp` repo
7. Note the **Installation ID** from the URL: `github.com/organizations/YOUR-ORG/settings/installations/INSTALLATION_ID`

#### Step 2: Store Credentials

**GitHub Secrets (for dev environment):**

1. Go to GitHub repo → **Settings → Secrets and variables → Actions**
2. Create these **repository secrets**:

| Secret Name                     | Value                                                                |
| ------------------------------- | -------------------------------------------------------------------- |
| `ARGOCD_GITHUB_APP_ID`          | Your App ID (e.g., `123456`)                                         |
| `ARGOCD_GITHUB_APP_PRIVATE_KEY` | Contents of the `.pem` file (paste as-is, including BEGIN/END lines) |

> 💡 **Keep the `.pem` file safe** - GitHub secrets cannot be retrieved once saved. Store it in a password manager.

#### How It Works Per Environment

| Environment | Credentials Source     | Token Type                                       |
| ----------- | ---------------------- | ------------------------------------------------ |
| **Dev**     | GitHub Actions secrets | Short-lived (1 hour) - OK for ephemeral clusters |
| **Staging** | AWS Secrets Manager    | GitHub App native (auto-refresh)                 |
| **Prod**    | AWS Secrets Manager    | GitHub App native (auto-refresh)                 |

---

## Operating the Cluster

For day-2 operations — observability dashboards, progressive delivery, encryption
verification, resource usage, cost monitoring, and Kubernetes upgrades — see
**[docs/operations.md](docs/operations.md)**.

