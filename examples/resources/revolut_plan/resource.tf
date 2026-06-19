# A Revolut subscription plan. NOTE: plans cannot be updated or deleted via the
# API — any change recreates the plan, and `terraform destroy` only removes it
# from state (the plan is orphaned in Revolut).
#
# Variations are a nested attribute of the plan (the Revolut Merchant API has no
# standalone plan-variation endpoint — variations only exist inside a plan).
resource "revolut_plan" "pro" {
  name           = "Pro"
  trial_duration = "P14D"

  variations = [
    {
      phases = [
        {
          ordinal        = 1
          cycle_duration = "P1M"

          # When a phase carries subscription_items, pricing lives on the items.
          # Every item requires name, unit and type; a flat item also requires
          # quantity; a usage item also requires code.
          subscription_items = [
            {
              name     = "Base"
              unit     = "month"
              type     = "flat"
              quantity = 1
              amount   = 900 # £9.00 in minor units
              currency = "GBP"
            }
          ]
        }
      ]
    }
  ]
}

output "pro_plan_id" {
  value = revolut_plan.pro.id
}
