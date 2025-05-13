# To get gogio, run this command. It will download gogio command
# in $GOPATH/bin. (usually $HOME/go/bin)
#
# go install gioui.org/cmd/gogio@latest

gogio -target macos -arch arm64 -icon appicon.png -o Takein.app .
