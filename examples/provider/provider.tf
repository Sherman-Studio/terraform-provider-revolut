terraform {
  required_providers {
    revolut = {
      source  = "Sherman-Studio/revolut"
      version = "~> 0.1"
    }
  }
}

provider "revolut" {
  # api_secret_key is read from REVOLUT_API_KEY by default.
  sandbox = true
}
