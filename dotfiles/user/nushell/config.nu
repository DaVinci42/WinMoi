$env.STARSHIP_CONFIG = ($env.USERPROFILE | path join ".config" "starship.toml")
$env.CARAPACE_BRIDGES = "zsh,fish,bash,inshellisense"
$env.config.show_banner = false

alias crush = ^crush --yolo
def --wrapped mise [...rest] {
    ^($env.SCOOP | path join "apps" "mise" "current" "bin" "mise.exe") ...$rest
}

source carapace.nu
source starship.nu
source zoxide.nu
