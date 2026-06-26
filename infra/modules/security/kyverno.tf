# Deploys Kyverno for policy enforcement.
# Enforces Pod Security Standards and detects anomalous container behavior.
# https://kyverno.io/docs/introduction/how-kyverno-works/

terraform {
  required_providers {
    kubectl = {
      source = "alekc/kubectl"
    }
  }
}

# IRSA role for Kyverno to authenticate to cross-account ECR when verifying image signatures.
# Without this, Kyverno's verify-image-signatures policy gets 401 from ECR.
module "kyverno_irsa" {
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts"
  version = "6.3.0"

  name = "${var.cluster_name}-kyverno"

  oidc_providers = {
    main = {
      provider_arn               = var.oidc_provider_arn
      namespace_service_accounts = ["kyverno:kyverno-admission-controller", "kyverno:kyverno-background-controller"]
    }
  }

  policies = {
    kyverno_ecr = aws_iam_policy.kyverno_ecr.arn
  }

  tags = {
    Purpose = "kyverno-image-verification"
  }
}

resource "aws_iam_policy" "kyverno_ecr" {
  name = "${var.cluster_name}-kyverno-ecr"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:BatchCheckLayerAvailability"
        ]
        Resource = ["arn:aws:ecr:*:${var.ecr_account_id}:repository/idp/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      }
    ]
  })
}

resource "helm_release" "kyverno" {
  name             = "kyverno"
  namespace        = "kyverno"
  create_namespace = true
  repository       = "https://kyverno.github.io/kyverno"
  chart            = "kyverno"
  version          = "3.6.2"

  timeout = 600

  values = [
    yamlencode({
      admissionController = {
        replicas = var.environment == "prod" ? 3 : 1
        serviceMonitor = {
          enabled = true
        }
        logging = {
          format    = "json"
          verbosity = 2 # Info level - captures admission decisions
        }
        serviceAccount = {
          annotations = {
            "eks.amazonaws.com/role-arn" = module.kyverno_irsa.arn
          }
        }
      }
      backgroundController = {
        replicas = var.environment == "prod" ? 2 : 1
        serviceMonitor = {
          enabled = true
        }
        logging = {
          format    = "json"
          verbosity = 2
        }
        serviceAccount = {
          annotations = {
            "eks.amazonaws.com/role-arn" = module.kyverno_irsa.arn
          }
        }
      }
      cleanupController = {
        replicas = 1
        serviceMonitor = {
          enabled = true
        }
      }
      reportsController = {
        replicas = 1
        serviceMonitor = {
          enabled = true
        }
      }
    })
  ]
}

locals {
  kyverno_policy_dir = "${path.module}/kyverno-policies"

  kyverno_policies = {
    for f in fileset(local.kyverno_policy_dir, "*.yaml") :
    trimsuffix(f, ".yaml") => yamldecode(file("${local.kyverno_policy_dir}/${f}"))
  }

  kyverno_validation_action = var.environment == "prod" ? "Enforce" : "Audit"
}

resource "kubectl_manifest" "kyverno_cluster_policy" {
  for_each = local.kyverno_policies

  yaml_body = yamlencode(merge(each.value, {
    spec = merge(each.value.spec, {
      validationFailureAction = local.kyverno_validation_action
    })
  }))

  depends_on = [helm_release.kyverno]
}

# Supply Chain Security: Verify image signatures (Cosign/Sigstore)
# Required for: FDA 21 CFR Part 11, PCI-DSS 6.3, SOC 2 CC8.1
resource "kubectl_manifest" "kyverno_verify_image_signatures" {
  yaml_body = yamlencode({
    apiVersion = "kyverno.io/v1"
    kind       = "ClusterPolicy"
    metadata = {
      name = "verify-image-signatures"
      annotations = {
        "policies.kyverno.io/title"       = "Verify Image Signatures"
        "policies.kyverno.io/category"    = "Supply Chain Security"
        "policies.kyverno.io/severity"    = "high"
        "policies.kyverno.io/description" = "Verify that container images are signed with Cosign. FDA 21 CFR Part 11, PCI-DSS 6.3"
      }
    }
    spec = {
      validationFailureAction = var.environment == "prod" ? "Enforce" : "Audit" # Enforce in prod, audit in dev/staging
      background              = true
      webhookTimeoutSeconds   = 30
      rules = [
        {
          name = "verify-signature"
          match = {
            any = [{ resources = { kinds = ["Pod"] } }]
          }
          exclude = {
            any = [
              { resources = { namespaces = ["kube-system", "kyverno", "argocd", "monitoring", "kubecost", "argo-rollouts", "external-secrets", "falco-system"] } }
            ]
          }
          verifyImages = [
            {
              # Verify images from our ECR repository are signed
              imageReferences = [
                "*.dkr.ecr.*.amazonaws.com/idp/*"
              ]
              mutateDigest = var.environment == "prod" ? true : false # Only mutate in prod for enforcement
              attestors = [
                {
                  count = 1
                  entries = [
                    {
                      keyless = {
                        # Sigstore keyless signing via GitHub Actions OIDC
                        # Only accepts signatures from workflows in the trusted GitHub org
                        issuer  = "https://token.actions.githubusercontent.com"
                        subject = "https://github.com/${var.trusted_github_org}/*"
                        rekor = {
                          url = "https://rekor.sigstore.dev"
                        }
                      }
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  })

  depends_on = [helm_release.kyverno]
}

