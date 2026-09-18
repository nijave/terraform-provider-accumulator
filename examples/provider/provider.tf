terraform {
  required_providers {
    accumulator = {
      source  = "nijave/accumulator"
      version = "~> 0.1"
    }
  }
}

# The provider takes no configuration. State is the only store, so there is no
# endpoint, no credential, and no client.
provider "accumulator" {}
