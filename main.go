package main

import (
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/markdown"
	"gioui.org/x/richtext"
	"github.com/BurntSushi/toml"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type Config struct {
	PathSepBy string
	PathKeys  string
	NameSepBy string
	NameKeys  string
	Dest      string
}

// UI는 프로그램 UI 구성에 필요한 정보들이다.
type UI struct {
	Program             *Program
	Window              *app.Window
	ConfigFile          string
	PathSeparatorEditor *widget.Editor
	PathKeyEditor       *widget.Editor
	NameSeparatorEditor *widget.Editor
	NameKeyEditor       *widget.Editor
	InputEditor         *widget.Editor
	DestEditor          *widget.Editor
	List                *widget.List
	Result              []richtext.SpanStyle
	ResultState         richtext.InteractiveText
	Theme               *material.Theme
	AnalyzeButton       *widget.Clickable
	CancelButton        *widget.Clickable
	RunButton           *widget.Clickable
	OKButton            *widget.Clickable
	FromRadio           *widget.Enum
	MethodRadio         *widget.Enum
	Notifier            *widget.Editor
	NotifyIsError       bool
	BorderColor         color.NRGBA
	DestColor           color.NRGBA
	DestHintColor       color.NRGBA
}

// Result는 복사후 결과를 표시하기 위한 정보이다.
type Result struct {
	Renderer    *markdown.Renderer
	Interaction richtext.InteractiveText
	Cache       []richtext.SpanStyle
}

// Loop는 이벤트가 발생할 때 마다 UI를 갱신한다.
func (ui *UI) Loop() error {
	var ops op.Ops
	for {
		e := ui.Window.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.HandleEvent(gtx)
			ui.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
	return nil
}

// HandleEvent는 발생한 이벤트에 맞게 UI 상태를 수정한다.
func (ui *UI) HandleEvent(gtx C) {
	ui.NotifyIsError = false
	dirty := false
	for _, ed := range []*widget.Editor{ui.InputEditor, ui.DestEditor, ui.PathSeparatorEditor, ui.PathKeyEditor, ui.NameSeparatorEditor, ui.NameKeyEditor} {
		for {
			event, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if reflect.DeepEqual(event, widget.ChangeEvent{}) {
				dirty = true
			}
		}
	}
	ui.Program.PathSeps = strings.Fields(ui.PathSeparatorEditor.Text())
	ui.Program.PathKeys = strings.Fields(ui.PathKeyEditor.Text())
	ui.Program.NameSeps = strings.Fields(ui.NameSeparatorEditor.Text())
	ui.Program.NameKeys = strings.Fields(ui.NameKeyEditor.Text())
	ui.Program.DestPattern = ui.DestEditor.Text()
	if dirty {
		ui.Validate()
	}
	if ui.AnalyzeButton.Clicked(gtx) {
		text := ui.InputEditor.Text()
		ui.Program.InputText = text
		err := ui.Program.AnalyzeInput(text)
		if err != nil {
			ui.Notifier.SetText(err.Error())
			ui.NotifyIsError = true
		} else {
			ui.Program.Analyzed = true
			analyzed := analyzeInput(ui.Program)
			ui.Result = analyzed
			ui.Notifier.SetText("path analyzed")
			ui.NotifyIsError = false
		}
	}
	if ui.OKButton.Clicked(gtx) {
		// make it ready to get a new input
		ui.Program.Analyzed = false
		ui.Program.Done = false
		// change to a fresh InputEditor.
		input := new(widget.Editor)
		ui.InputEditor = input
	}
	if ui.CancelButton.Clicked(gtx) {
		// let user modify input
		ui.Program.Analyzed = false
		ui.Program.Done = false
		ui.Notifier.SetText("please modify your paths and analyze again")
		ui.NotifyIsError = false
	}
	if ui.RunButton.Clicked(gtx) {
		err := ui.Program.Copy()
		if err != nil {
			ui.Notifier.SetText(err.Error())
			ui.NotifyIsError = true
		} else {
			ui.Result = analyzeCopy(ui.Program)
			ui.Notifier.SetText("done")
			ui.NotifyIsError = false
			ui.Program.Done = true
			// save the lastest setting
			cfg := &Config{
				PathSepBy: ui.PathSeparatorEditor.Text(),
				PathKeys:  ui.PathKeyEditor.Text(),
				NameSepBy: ui.NameSeparatorEditor.Text(),
				NameKeys:  ui.NameKeyEditor.Text(),
				Dest:      ui.DestEditor.Text(),
			}
			os.MkdirAll(filepath.Dir(ui.ConfigFile), 0755)
			f, err := os.Create(ui.ConfigFile)
			if err != nil {
				ui.Notifier.SetText(err.Error())
				ui.NotifyIsError = true
			}
			err = toml.NewEncoder(f).Encode(cfg)
			if err != nil {
				ui.Notifier.SetText(err.Error())
				ui.NotifyIsError = true
			}
			f.Close()
		}
	}
	for {
		span, event, ok := ui.ResultState.Update(gtx)
		if !ok {
			break
		}
		path, _ := span.Content()
		switch event.Type {
		case richtext.Click:
			openCmd := map[string]string{
				"darwin": "open",
				"linux":  "xdg-open",
			}[runtime.GOOS]
			if openCmd == "" {
				return
			}
			cmd := exec.Command(openCmd, path)
			err := cmd.Start()
			if err != nil {
				ui.Notifier.SetText(err.Error())
				ui.NotifyIsError = false
			}
		}
	}
	ui.DestEditor.ReadOnly = false
	ui.BorderColor = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	ui.DestColor = color.NRGBA{A: 255}
	ui.DestHintColor = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	if ui.Program.Analyzed || ui.Program.Done {
		ui.DestEditor.ReadOnly = true
		ui.DestColor = color.NRGBA{R: 160, G: 160, B: 160, A: 255}
		ui.DestHintColor = color.NRGBA{}
	}
}

func (ui *UI) Validate() {
	dest := strings.TrimSpace(ui.DestEditor.Text())
	if dest == "" {
		ui.Notifier.SetText("please set destination")
		ui.NotifyIsError = true
		return
	}
	dest = os.ExpandEnv(dest)
	if !strings.HasPrefix(dest, "/") {
		ui.Notifier.SetText("destination path cannot be relative")
		ui.NotifyIsError = true
		return
	}
	if strings.TrimSpace(ui.InputEditor.Text()) == "" {
		ui.Notifier.SetText("paste filepaths to take in.")
		ui.NotifyIsError = false
		return
	}
	sampleSrc := ""
	lines := strings.Split(ui.InputEditor.Text(), "\n")
	for _, l := range lines {
		l = strings.TrimPrefix(l, "file://")
		if strings.HasPrefix(l, "/") {
			sampleSrc = l
			break
		}
	}
	if sampleSrc == "" {
		ui.Notifier.SetText("filepath not found")
		ui.NotifyIsError = false
		return
	}
	env, err := ui.Program.ParseEnvsFromSrc(sampleSrc)
	if err != nil {
		ui.Notifier.SetText("dest sample: " + err.Error())
		return
	}
	env["DATE"] = time.Now().Format("060102")
	sampleDest, err := destDirectory(sampleSrc, ui.DestEditor.Text(), env)
	if err != nil {
		ui.Notifier.SetText("dest sample: " + err.Error())
		return
	}
	ui.Notifier.SetText("dest sample: " + sampleDest)
}

// Layout은 현재 UI 상태에 따라 레이아웃을 설정한다.
func (ui *UI) Layout(gtx C) D {
	return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return material.Body1(ui.Theme, "separate path to ").Layout(gtx) }),
					layout.Flexed(1, func(gtx C) D {
						return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
								med := material.Editor(ui.Theme, ui.PathKeyEditor, "path environ filter")
								med.Color = ui.DestColor
								med.HintColor = ui.DestHintColor
								return med.Layout(gtx)
							})
						})
					}),
					layout.Rigid(func(gtx C) D { return material.Body1(ui.Theme, " with ").Layout(gtx) }),
					layout.Rigid(func(gtx C) D {
						return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
								gtx.Constraints.Max.X = 150
								med := material.Editor(ui.Theme, ui.PathSeparatorEditor, "separators (/ \\)")
								med.Color = ui.DestColor
								med.HintColor = ui.DestHintColor
								return med.Layout(gtx)
							})
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return material.Body1(ui.Theme, "separate name to ").Layout(gtx) }),
					layout.Flexed(1, func(gtx C) D {
						return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
								med := material.Editor(ui.Theme, ui.NameKeyEditor, "name environ filter")
								med.Color = ui.DestColor
								med.HintColor = ui.DestHintColor
								return med.Layout(gtx)
							})
						})
					}),
					layout.Rigid(func(gtx C) D { return material.Body1(ui.Theme, " with ").Layout(gtx) }),
					layout.Rigid(func(gtx C) D {
						return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
								gtx.Constraints.Max.X = 150
								med := material.Editor(ui.Theme, ui.NameSeparatorEditor, "separators (_ .)")
								med.Color = ui.DestColor
								med.HintColor = ui.DestHintColor
								return med.Layout(gtx)
							})
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
					return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
						if !ui.Program.Analyzed {
							return material.Editor(ui.Theme, ui.InputEditor, "paths to copy").Layout(gtx)
						} else {
							return material.List(ui.Theme, ui.List).Layout(gtx, 1, func(gtx C, i int) D {
								return richtext.Text(&ui.ResultState, ui.Theme.Shaper, ui.Result...).Layout(gtx)
							})
						}
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
			layout.Rigid(func(gtx C) D {
				return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
					return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx C) D {
						med := material.Editor(ui.Theme, ui.DestEditor, "destination folder")
						med.Color = ui.DestColor
						med.HintColor = ui.DestHintColor
						return med.Layout(gtx)
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
			layout.Rigid(func(gtx C) D {
				childs := []layout.FlexChild{
					layout.Rigid(material.RadioButton(ui.Theme, ui.MethodRadio, "link", "Link").Layout),
					layout.Rigid(material.RadioButton(ui.Theme, ui.MethodRadio, "copy", "Copy").Layout),
				}
				childs = append(childs, layout.Rigid(layout.Spacer{Width: unit.Dp(20)}.Layout))
				childs = append(childs, layout.Flexed(1, layout.Spacer{}.Layout))
				if ui.Program.Done {
					childs = append(childs, layout.Rigid(material.Button(ui.Theme, ui.OKButton, "OK").Layout))
				} else if ui.Program.Analyzed {
					childs = append(childs, layout.Rigid(material.Button(ui.Theme, ui.CancelButton, "Cancel").Layout))
					childs = append(childs, layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout))
					childs = append(childs, layout.Rigid(material.Button(ui.Theme, ui.RunButton, "Run").Layout))
				} else {
					childs = append(childs, layout.Rigid(material.Button(ui.Theme, ui.AnalyzeButton, "Analyze").Layout))
				}
				return layout.Flex{}.Layout(gtx,
					childs...,
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx C) D {
				return widget.Border{Color: ui.BorderColor, CornerRadius: unit.Dp(1), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
					med := material.Editor(ui.Theme, ui.Notifier, "")
					if ui.NotifyIsError {
						med.Color = color.NRGBA{R: 192, G: 32, B: 32, A: 255}
					}
					return layout.UniformInset(unit.Dp(2)).Layout(gtx,
						med.Layout,
					)
				})
			}),
		)
	})
}

// Program은 받아들인 경로를 다양한 각도에서 분석한 정보이다.
type Program struct {
	InputText       string
	PathSeps        []string
	PathKeys        []string
	NameSeps        []string
	NameKeys        []string
	DestPattern     string
	Method          string
	Analyzed        bool
	Done            bool
	NotExists       []string
	Invalids        []string
	Srcs            []string
	SrcIsDir        map[string]bool
	SrcDirFileCount map[string]int
	DestDir         map[string]string
	DestDirSrcs     map[string][]string
	DestDirExists   map[string]bool
	Today           string
}

func (p *Program) ParseEnvsFromSrc(src string) (map[string]string, error) {
	env := make(map[string]string)
	pathEnv, err := parseEnvs(src, p.PathSeps, p.PathKeys)
	if err != nil {
		return nil, err
	}
	for k, v := range pathEnv {
		env[k] = v
	}
	nameEnv, err := parseEnvs(filepath.Base(src), p.NameSeps, p.NameKeys)
	if err != nil {
		return nil, err
	}
	for k, v := range nameEnv {
		env[k] = v
	}
	return env, nil
}

// Analyze는 사용자가 입력한 텍스트를 받아들이고 그 안에서 경로를 찾아
// 그 상태 및 대상 경로 정보 분석한다.
func (p *Program) AnalyzeInput(text string) error {
	// 이전 데이터 삭제
	p.NotExists = make([]string, 0)
	p.Invalids = make([]string, 0)
	p.Srcs = make([]string, 0)
	p.SrcIsDir = make(map[string]bool)
	p.SrcDirFileCount = make(map[string]int)
	p.DestDir = make(map[string]string)
	p.DestDirSrcs = make(map[string][]string)
	p.DestDirExists = make(map[string]bool)
	p.Today = time.Now().Format("060102")
	// 문자열에서 경로 추출
	text = strings.Replace(text, "\r\n", "\n", -1)
	lines := strings.Split(text, "\n")
	paths := make([]string, 0)
	for _, l := range lines {
		l = strings.TrimPrefix(l, "file://")
		if strings.HasPrefix(l, "/") {
			// 할일: 윈도우즈 경로형식 처리
			paths = append(paths, l)
		}
	}
	// 경로 분석
	//
	// 존재하는 파일과 존재하지 않는 파일 분리
	for _, src := range paths {
		src = strings.TrimSpace(src)
		fi, err := os.Stat(src)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%v: %s", err, src)
			}
			p.NotExists = append(p.NotExists, src)
			continue
		}
		p.Srcs = append(p.Srcs, src)
		p.SrcIsDir[src] = fi.IsDir()
	}
	sort.Strings(p.Srcs)
	// 소스 경로에 대한 대상 경로를 찾고, 찾지 못하거나 문제가 있으면 유효하지 않은 것으로 간주
	for _, src := range p.Srcs {
		env, err := p.ParseEnvsFromSrc(src)
		if err != nil {
			return err
		}
		env["DATE"] = p.Today
		destDir, err := destDirectory(src, p.DestPattern, env)
		if err != nil {
			p.Invalids = append(p.Invalids, src+" ("+err.Error()+")")
			continue
		}
		p.DestDir[src] = destDir
		// 소스 경로가 디렉토리이면 그 안의 파일 갯수 분석
		if p.SrcIsDir[src] {
			srcd := os.DirFS(src)
			err := fs.WalkDir(srcd, ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				p.SrcDirFileCount[src] += 1
				// 1000개 이상의 파일이 있다면 더이상 세지 않는다.
				// 복사 단계에서는 모든 파일이 복사될 것이다.
				if p.SrcDirFileCount[src] > 1000 {
					return fs.SkipDir
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		// 대상 경로가 복사될 디렉토리가 이미 존재하는지 검사
		if _, checked := p.DestDirExists[destDir]; !checked {
			_, err := os.Stat(destDir)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%v: %s", err, src)
				}
				p.DestDirExists[destDir] = false
			} else {
				p.DestDirExists[destDir] = true
			}
		}
		destDirSrcs := p.DestDirSrcs[destDir]
		if destDirSrcs == nil {
			destDirSrcs = make([]string, 0)
		}
		destDirSrcs = append(destDirSrcs, src)
		p.DestDirSrcs[destDir] = destDirSrcs
	}
	return nil
}

func richTitle(text string) richtext.SpanStyle {
	return richtext.SpanStyle{
		Content: text,
		Color:   color.NRGBA{A: 255},
		Size:    unit.Sp(20),
		Font:    gofont.Collection()[0].Font,
	}
}

func richTitlePath(text string) richtext.SpanStyle {
	return richtext.SpanStyle{
		Content:     text,
		Color:       color.NRGBA{A: 255},
		Size:        unit.Sp(20),
		Font:        gofont.Collection()[0].Font,
		Interactive: true,
	}
}

func richPath(text string) richtext.SpanStyle {
	return richtext.SpanStyle{
		Content:     text,
		Color:       color.NRGBA{A: 255, B: 170},
		Size:        unit.Sp(15),
		Font:        gofont.Collection()[0].Font,
		Interactive: true,
	}
}

func richText(text string) richtext.SpanStyle {
	return richtext.SpanStyle{
		Content: text,
		Color:   color.NRGBA{A: 255},
		Size:    unit.Sp(15),
		Font:    gofont.Collection()[0].Font,
	}
}

// 인풋을 분석한 프로그램 정보를 바탕으로 사용자에게 알려줄 정보를 생성한다.
func analyzeInput(p *Program) []richtext.SpanStyle {
	res := make([]richtext.SpanStyle, 0)
	if len(p.NotExists) != 0 {
		res = append(res, richTitle("Not Exists"))
		res = append(res, richText("\n"))
		for _, path := range p.NotExists {
			res = append(res, richPath(path))
			res = append(res, richText("\n"))
		}
		res = append(res, richText("\n"))
	}
	if len(p.Invalids) != 0 {
		res = append(res, richTitle("Invalids"))
		res = append(res, richText("\n"))
		for _, path := range p.Invalids {
			res = append(res, richPath(path))
			res = append(res, richText("\n"))
		}
		res = append(res, richText("\n"))
	}
	destDirs := make([]string, 0, len(p.DestDirSrcs))
	for dd := range p.DestDirSrcs {
		destDirs = append(destDirs, dd)
	}
	for _, dd := range destDirs {
		res = append(res, richTitle("To: "))
		res = append(res, richTitlePath(dd))
		exist := p.DestDirExists[dd]
		if !exist {
			res = append(res, richTitle(" "+"(to be created)"))
		}
		res = append(res, richText("\n"))
		srcs := p.DestDirSrcs[dd]
		for _, src := range srcs {
			line := ""
			// dest := p.DestDir[src]
			srcName := filepath.Base(src)
			destName := srcName // TODO: 이름 변환 지원
			res = append(res, richPath(src))
			comment := ""
			if p.SrcIsDir[src] {
				count := p.SrcDirFileCount[src]
				counts := strconv.Itoa(count)
				if count > 1000 {
					// 1000개 이상의 파일이 있어 더이상 세지 않았다.
					// 복사 단계에서는 모든 파일이 복사될 것이다.
					counts = "1000+"
				}
				plural := ""
				if count > 1 {
					plural = "s"
				}
				comment += "directory, containing " + counts + " file" + plural
			}
			if srcName != destName {
				if comment != "" {
					comment += " "
				}
				comment += destName
			}
			if comment != "" {
				line += " (" + comment + ")"
			}
			res = append(res, richText(line))
			res = append(res, richText("\n"))
		}
		res = append(res, richText("\n"))
	}
	return res
}

func analyzeCopy(p *Program) []richtext.SpanStyle {
	res := make([]richtext.SpanStyle, 0)
	res = append(res, richTitle("Copy completed"))
	res = append(res, richText("\n\n"))
	for destDir, srcs := range p.DestDirSrcs {
		res = append(res, richTitle("Copied: "))
		res = append(res, richTitlePath(destDir))
		res = append(res, richText("\n"))
		for _, src := range srcs {
			res = append(res, richPath(destDir+filepath.Base(src)))
			res = append(res, richText("\n"))
		}
	}
	return res
}

// Copy는 프로그램 설정에 따라 분석한 소스 파일을 대상 경로로 복사한다.
func (p *Program) Copy() error {
	if !p.Analyzed {
		return fmt.Errorf("paths not analyzed yet")
	}
	copyFunc := os.Link
	if p.Method == "copy" {
		copyFunc = copyFile
	}
	for destDir, srcs := range p.DestDirSrcs {
		// 소스에서 그 안의 모든 파일 경로를 분석한다.
		// 혹시 복사 방법이 링크일 때 디렉토리 소스를 바로 링크하지 않고
		// 그 안의 개별 파일들을 링크하는 방식을 사용하면
		// 복사된 경로에서 실수로 파일을 지우는 것을 방지할수 있기 때문이다.
		// 개별 파일을 링크한다면 그 안의 내용물을 지워도
		// 소스 파일 정보가 삭제되지 않는다.
		subPath := make(map[string]string)
		for _, src := range srcs {
			if p.SrcIsDir[src] {
				srcDir := filepath.Dir(src)
				filepath.WalkDir(src, func(s string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					sub := s[len(srcDir):]
					subPath[s] = sub
					return nil
				})
			} else {
				subPath[src] = filepath.Base(src)
			}
		}
		// 링크 또는 복사 수행
		for s, sub := range subPath {
			d := filepath.Join(destDir, sub)
			dDir := filepath.Dir(d)
			_, err := os.Stat(dDir)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%v: %s", err, dDir)
				}
				err := os.MkdirAll(dDir, 0755)
				if err != nil {
					return fmt.Errorf("make dirs: %v: %s", err, dDir)
				}
			}
			_, err = os.Lstat(d)
			if err == nil {
				// 파일이 이미 존재한다.
				// 할일: 사용자가 원하면 덮어쓰기 기능을 제공해야 할까?
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%v: %s", err, s)
			}
			err = copyFunc(s, d)
			if err != nil {
				return fmt.Errorf("%s file: %v", p.Method, err)
			}
		}
	}
	return nil
}

func parseEnvs(src string, seps []string, keys []string) (map[string]string, error) {
	vals := make([]string, 0)
	remain := src
	for len(remain) > 0 {
		idx := len(remain)
		cutter := ""
		for _, sep := range seps {
			if strings.TrimSpace(sep) == "" {
				// spaces doesn't count as a separater
				continue
			}
			i := strings.Index(remain, sep)
			if i < 0 || i >= idx {
				continue
			}
			idx = i
			cutter = sep
		}
		vals = append(vals, remain[:idx])
		remain = remain[idx+len(cutter):]
	}
	idx := -1
	for i, key := range keys {
		if key == "..." {
			if idx != -1 {
				return nil, fmt.Errorf("multiple key divider (...) is not allowed")
			}
			idx = i
		}
	}
	var leftKeys, rightKeys []string
	if idx == -1 {
		if len(vals) > len(keys) {
			return nil, fmt.Errorf("too many values for keys: %s", src)
		}
		if len(vals) < len(keys) {
			return nil, fmt.Errorf("not enough values for keys: %s", src)
		}
		leftKeys = keys
	} else {
		if len(vals) < len(keys)-1 {
			return nil, fmt.Errorf("not enough values for keys: %s", src)
		}
		leftKeys = keys[:idx]
		rightKeys = keys[idx+1:]
	}
	envs := make(map[string]string)
	for i := range leftKeys {
		k := leftKeys[i]
		if k == "_" {
			continue
		}
		v := vals[i]
		envs[k] = v
	}
	for i := range rightKeys {
		// index from right
		k := rightKeys[len(rightKeys)-1-i]
		if k == "_" {
			continue
		}
		v := vals[len(vals)-1-i]
		envs[k] = v
	}
	return envs, nil
}

// destDirectory는 destPattern을 이용해 소스 경로를 복사할 폴더 경로를 반환한다.
func destDirectory(src, destPattern string, env map[string]string) (string, error) {
	if !filepath.IsAbs(src) {
		return "", fmt.Errorf("not an absolute path: %s", src)
	}
	destPattern = strings.TrimSpace(destPattern)
	unknown := ""
	destDir := os.Expand(destPattern, func(k string) string {
		v, ok := env[k]
		if !ok {
			unknown = "$" + k
			return ""
		}
		return v
	})
	if unknown != "" {
		return "", fmt.Errorf("unknown environ variable in dest: %s", unknown)
	}
	return destDir, nil
}

// stringMapper은 mapstr을 이용해 특정 문자열을 다른 문자열에 대응하는 맵을 만든다.
// mapstr은 각 문자열 토큰을 콤마(,)로 구분하고 2개를 하나의 쌍으로 놓아야 한다.
//
// 만일 토큰의 수가 2의 배수가 아니라면 마지막 하나는 사용되지 않는다.
// 예) "a,b,c,d,e" 가 mapstr의 값이면 이 매퍼는 a를 b로 c를 d로 변경하고, e는 버려진다.
func stringMapper(mapstr string) map[string]string {
	mapper := make(map[string]string)
	toks := strings.Split(mapstr, ",")
	for i := 0; i+1 < len(toks); i += 2 {
		from := toks[i]
		to := toks[i+1]
		mapper[from] = to
	}
	return mapper
}

// copyFile은 파일을 복사하고 복사중 에러가 났다면 그 내용을 반환한다.
func copyFile(src, dest string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = d.ReadFrom(s)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("couldn't find home dir")
	}
	cfg := &Config{
		PathSepBy: "/",
		PathKeys:  "_ _ _ _ SHOW ... NAME",
		NameSepBy: ". _",
		NameKeys:  "SEQ SCENE SHOT PART VER ...",
		Dest:      "/mnt/storm/show/${SHOW}/shot/${SEQ}/${SCENE}_${SHOT}/out/",
	}
	cfgFile := filepath.Join(cfgDir, "takein", "config.toml")
	_, err = os.Stat(cfgFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatal(err)
		}
	} else {
		_, err = toml.DecodeFile(cfgFile, &cfg)
		if err != nil {
			log.Fatal(err)
		}
	}
	w := new(app.Window)
	w.Option(app.Title("Takein"))
	prog := &Program{
		Analyzed: false,
	}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	pathSepEd := new(widget.Editor)
	pathSepEd.SingleLine = true
	pathSepEd.SetText(cfg.PathSepBy)
	pathKeyEd := new(widget.Editor)
	pathKeyEd.SetText(cfg.PathKeys)
	pathKeyEd.SingleLine = true
	nameSepEd := new(widget.Editor)
	nameSepEd.SetText(cfg.NameSepBy)
	nameSepEd.SingleLine = true
	nameKeyEd := new(widget.Editor)
	nameKeyEd.SetText(cfg.NameKeys)
	nameKeyEd.SingleLine = true
	input := new(widget.Editor)
	// display only shows the result.
	// by separating it, we can keep history of the editor clean.
	dest := new(widget.Editor)
	dest.SingleLine = true
	dest.SetText(cfg.Dest)
	analyzeBtn := new(widget.Clickable)
	cancelBtn := new(widget.Clickable)
	runBtn := new(widget.Clickable)
	okBtn := new(widget.Clickable)
	methodRad := new(widget.Enum)
	methodRad.Value = "link"
	notifier := new(widget.Editor)
	notifier.SingleLine = true
	notifier.ReadOnly = true
	ui := &UI{
		Program:             prog,
		Window:              w,
		Theme:               th,
		ConfigFile:          cfgFile,
		PathSeparatorEditor: pathSepEd,
		PathKeyEditor:       pathKeyEd,
		NameSeparatorEditor: nameSepEd,
		NameKeyEditor:       nameKeyEd,
		InputEditor:         input,
		DestEditor:          dest,
		List:                &widget.List{List: layout.List{Axis: layout.Vertical}},
		AnalyzeButton:       analyzeBtn,
		CancelButton:        cancelBtn,
		RunButton:           runBtn,
		OKButton:            okBtn,
		MethodRadio:         methodRad,
		Notifier:            notifier,
	}
	go func() {
		err := ui.Loop()
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
