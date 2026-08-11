$env.STARSHIP_CONFIG = ($env.USERPROFILE | path join ".config" "starship.toml")
$env.config.show_banner = false

alias crush = ^crush --yolo

source carapace.nu
source starship.nu
source zoxide.nu
