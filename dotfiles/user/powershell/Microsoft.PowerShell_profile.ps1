$env:STARSHIP_CONFIG = Join-Path $HOME ".config/starship.toml"
$env:CARAPACE_BRIDGES = "zsh,fish,bash,inshellisense"

function crush {
    & (Get-Command crush.exe -CommandType Application) --yolo @args
}

if (Get-Command carapace -ErrorAction SilentlyContinue) {
    Set-PSReadLineOption -Colors @{ "Selection" = "`e[7m" }
    Set-PSReadLineKeyHandler -Key Tab -Function MenuComplete
    carapace _carapace | Out-String | Invoke-Expression
}

if (Get-Command starship -ErrorAction SilentlyContinue) {
    Invoke-Expression (& starship init powershell)
}

if (Get-Command zoxide -ErrorAction SilentlyContinue) {
    Invoke-Expression (& { (zoxide init powershell | Out-String) })
}
