terraform {
  source = "${get_terragrunt_dir()}/terraform"
}

# Store state in /tmp so the repo stays clean between runs.
remote_state {
  backend = "local"

  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }

  config = {
    path = "/tmp/dumpscript-kind-e2e.tfstate"
  }
}
