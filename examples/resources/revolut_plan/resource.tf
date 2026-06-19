# A Revolut subscription plan. NOTE: plans cannot be updated or deleted via the
# API — any change recreates the plan, and `terraform destroy` only removes it
# from state (the plan is orphaned in Revolut).
resource "revolut_plan" "pro" {
  name           = "Pro"
  trial_duration = "P14D"

  variations {
    trial_duration = "P14D"

    phases {
      ordinal        = 1
      cycle_duration = "P1M"
      amount         = 900 # £9.00 in minor units
      currency       = "GBP"

      subscription_items {
        type   = "flat"
        amount = 900
      }
    }
  }
}

output "pro_plan_id" {
  value = revolut_plan.pro.id
}
