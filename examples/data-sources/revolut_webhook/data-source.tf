data "revolut_webhook" "existing" {
  id = "11111111-2222-3333-4444-555555555555"
}

output "webhook_events" {
  value = data.revolut_webhook.existing.events
}
