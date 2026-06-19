resource "revolut_webhook" "orders" {
  url    = "https://example.com/webhooks/revolut"
  events = ["ORDER_COMPLETED", "ORDER_AUTHORISED"]

  # Change this keeper to rotate the signing secret. expiration_period sets the
  # grace window during which the previous secret stays valid.
  rotate_trigger    = "v1"
  expiration_period = "PT5H30M"
}

output "webhook_signing_secret" {
  value     = revolut_webhook.orders.signing_secret
  sensitive = true
}
