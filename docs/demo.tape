# vhs script for the README demo GIF — https://github.com/charmbracelet/vhs
# Regenerate with: vhs docs/demo.tape   (run from a scratch repo with dibs on PATH)

Output docs/demo.gif

Set FontSize 16
Set Width 1100
Set Height 620
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Type "# two agents, one repo — watch them NOT collide" Enter Sleep 1s

Type `dibs claim src/auth --reason "refactor auth" --agent alice` Enter Sleep 1.5s

Type `dibs claim src/auth/token.go --agent bob --reason "fix token bug"` Enter Sleep 2.5s

Type `dibs claim src/api --agent bob --reason "new endpoints"` Enter Sleep 1.5s

Type "dibs status" Enter Sleep 3s

Type `dibs note "auth API is changing — don't regenerate clients yet" --agent alice` Enter Sleep 1.5s

Type "dibs notes --agent bob" Enter Sleep 2.5s

Type "# and if an agent ignores the rules, the hook stops it:" Enter Sleep 1s
Type `echo '{"tool_name":"Edit","tool_input":{"file_path":"src/auth/token.go"}}' | dibs hook claude --agent bob` Enter Sleep 4s
