package shellpolicy

// US-134 AC-1 and AC-13, the fourth round of design review security (F-22 to
// F-24), on the rule us134_test.go pins, commands-run-as-configured. The
// person's decision (option A): the rule asks agentenv.go's whole reading of
// the line, with a name test that takes any name for a protected one, rather
// than sharing some of its readers. So every spelling this package's tests
// expect refused for a protected name is replayed here with an unprotected
// name in its place, before both calls, and must be refused under the rule.
// The list is read from the test files themselves, as the US-129 corpus is,
// so that a reading agentenv.go gains is a reading this rule is held to.
//
// F-22: a builtin reached through a program word the shell makes, which
// agentenv.go reads as a made program given a word, and no clause of the rule
// read. F-23: PowerShell's Start-Process given an environment, and the env:
// drive named alone, which agentenv.go reads whatever the name (Start-Process
// given an environment is refused today, so its rows are controls). F-24: bash's
// keyword option turned on through shopt, or through an option word the shell
// makes. The four spellings the review lists as trunk-shared (F-25) have no
// row here: a made whole call behind an allowed word or a keyword, quoted
// expansions that split, a made eval, and a made subcommand behind a wrapper.
//
// The fifth round (F-26) adds .NET's SetEnvironmentVariable reached by
// reflection, by a quoted or built method name, or by Invoke: agentenv.go
// reads those through quotesProtected, which is not asked with any name, so
// the method's name is what the rule must read. iex given code that builds a
// setting (F-27) is trunk-shared and has no row.
//
// The sixth round (F-29) found two more routes that the named lookups missed.
// The person chose the class rule: on a PowerShell line that holds the call,
// any .NET type or member is a change. So the fifth round's three reads of
// [Environment] are now refused rows, not guards.
//
// The seventh round (F-30) found member access without a dot (ForEach-Object
// given a method's name) and an Env: path built from quoted parts. The person
// chose the line rule: on PowerShell the call stands alone on its line, and
// anything beside it is refused. So every PowerShell guard that allowed
// something beside the call is now a refused row. Each was asked in
// PowerShell sessions only, so none had a bash half to keep, and bash's
// readings are unchanged.
//
// The eighth round (security F-35, F-36 and F-37, architect F-13) found lines
// that run the call without being read as holding it: a parenthesised
// operand or program, which the reader splits into a run of its own; a
// process given its arguments before sdlc's name; and pwsh's encoded
// command. The person's rule: on PowerShell, a line whose program is not a
// literal word, the call operator, Start-Process or a .NET Process naming
// sdlc in any order, Invoke-Expression, or pwsh given an encoded command
// may be the call, and must be the call alone. One prefix is allowed, so
// that a session can run the call in the story's worktree: Set-Location or
// cd with a plain or single-quoted absolute path, then ; or &&.
//
// The ninth round (F-39 to F-41) found literal text naming the call run in
// ways no list held, statements nested in blocks and subexpressions, and the
// encoded-command switch spelt with other dashes. The person's rule: on
// PowerShell, any line naming sdlc and the call's words, in any order, may
// be the call; statements are read inside every block and subexpression;
// and the switch is read after any dash form. The US-129 golden's one prose
// line that names sdlc gate and freeze was written again for it.
//
// The tenth round (F-43 and F-44) found pwsh handed its switch or script
// through a splat, a variable or a decoding expression, and statements hidden
// between typographic quotes, which PowerShell closes a string at. The
// person's rule: pwsh or powershell given anything but literal words, and
// any line holding a typographic quote, may be the call.
//
// How the rows were tried (2026-11-22, bash 5.3.20 by full path and zsh 5.9
// with -f, on macOS), with a stand-in sdlc first on PATH and another under
// fake: each row's comment says what it changed in which shell. The
// PowerShell rows were not run, as no PowerShell was at hand. Every refused
// row fails on today's code, which allows the line; the guards pass on it.
// All pass against a prototype in a scratch copy.

import (
	"encoding/base64"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// us134AgentenvRule is the rule agentenv.go refuses a protected name under.
const us134AgentenvRule = "enforcement-stays-on"

// us134Unprotected is s with every protected name in it replaced by GOFLAGS,
// and every start of one that a pattern, an escape, a quote or an expansion
// cuts short (`AI_*`, `AI_\AGENT`, `AI_AGEN[T]`) made the start of a name
// nobody protects, its first two letters swapped for ZZ. A start that runs on
// to a name of its own (CLAUDE_CODE_SESSION_ID) is another variable and is
// left as it is. The names are read from protectedNames, so that this file
// names none of them and cannot drift from the list.
func us134Unprotected(s string) string {
	for _, name := range protectedNames {
		full := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
		s = full.ReplaceAllStringFunc(s, func(m string) string {
			if m == strings.ToLower(m) {
				return "goflags"
			}
			return "GOFLAGS"
		})
	}
	for _, name := range protectedNames {
		prefix := name[:strings.LastIndexByte(name, '_')+1]
		start := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(prefix) + `\w*`)
		var b strings.Builder
		last := 0
		for _, at := range start.FindAllStringIndex(s, -1) {
			cut := at[1] < len(s) && strings.IndexByte("\\*?[(|<^#~@'\"$`{", s[at[1]]) >= 0
			if !cut || !strings.HasPrefix(name, strings.ToUpper(s[at[0]:at[1]])) {
				continue
			}
			zz := "ZZ"
			if s[at[0]:at[0]+2] == strings.ToLower(s[at[0]:at[0]+2]) {
				zz = "zz"
			}
			b.WriteString(s[last:at[0]] + zz)
			last = at[0] + 2
		}
		s = b.String() + s[last:]
	}
	return s
}

// us134Replayed is a spelling refused for a protected name, with an
// unprotected one in its place, and the shell it was refused from.
type us134Replayed struct {
	file, spelling string
	powerShell     bool
}

// us134ProtectedSpellings is every string literal of this package's test
// files (but US-129's corpus and US-134's own) that Inspect refuses as
// enforcement-stays-on, from Bash or from PowerShell, and that it no longer
// refuses with the name made unprotected: the spellings refused for the name
// they name. A spelling refused whatever the name (`env -i`, `unset "$V"`) is
// held for every name already, and one refused under another rule with the
// name taken out is that rule's; neither is replayed.
func us134ProtectedSpellings(t *testing.T) []us134Replayed {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	seen := map[string]bool{}
	var out []us134Replayed
	fset := gotoken.NewFileSet()
	for _, name := range files {
		if strings.HasPrefix(name, "us129_") || strings.HasPrefix(name, "us134") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != gotoken.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || strings.TrimSpace(s) == "" || seen[s] {
				return true
			}
			seen[s] = true
			u := us134Unprotected(s)
			if u == s {
				return true
			}
			for _, ps := range []bool{false, true} {
				st := State{CommitReady: true, PowerShell: ps}
				if f, ok := Inspect(s, st); !ok || f.Rule != us134AgentenvRule {
					continue
				}
				if _, ok := Inspect(u, st); ok {
					continue
				}
				out = append(out, us134Replayed{file: name, spelling: u, powerShell: ps})
			}
			return true
		})
	}
	return out
}

// AC-1 and AC-13 (F-22 to F-24, option A): every spelling agentenv.go's tests
// expect refused for a protected name is, with GOFLAGS or another unprotected
// name in its place and on a line before either call, refused under the rule.
// A spelling that is a fragment of a longer row may leave the line to another
// rule: where the same line ending in `sdlc status`, which runs no configured
// command, is refused already, that rule holds it and the row is not asked.
func TestAC1AC13EverySpellingRefusedForAProtectedNameIsAChangeForAnyName(t *testing.T) {
	spellings := us134ProtectedSpellings(t)
	// The harvest found 141 from each shell on 2026-11-22, 142 spellings in
	// all, from forgedevent_test.go, hiddenbehind_test.go, oneline_test.go,
	// readings_test.go and us130_test.go; four of Bash's are fragments of an
	// interpreter's code left to another rule. A floor well below it, so that
	// a harvest that reads nothing does not pass, and a row added to or taken
	// from agentenv.go's tests moves nothing here.
	for _, ps := range []bool{false, true} {
		n := 0
		for _, sp := range spellings {
			if sp.powerShell == ps {
				n++
			}
		}
		if n < 100 {
			t.Fatalf("AC-1 AC-13 only %d spellings refused for a protected name were found (PowerShell %v); the harvest reads nothing", n, ps)
		}
	}
	for _, sp := range spellings {
		st := State{CommitReady: true, PowerShell: sp.powerShell}
		if _, ok := Inspect(sp.spelling+"\nsdlc status", st); ok {
			continue
		}
		for _, call := range us134Calls {
			command := sp.spelling + "\n" + strings.Replace(call, "@SDLC@", "sdlc", 1)
			refusedAsAChangedEnvironment(t, command, sp.powerShell)
		}
	}
}

// AC-1 and AC-13 (F-22): a builtin reached through a program word the shell
// makes. agentenv.go reads a made program given a word as one that may take
// a protected name away; the rule reads it as a change of any name. In both
// shells the made export of PATH had the stand-in under fake found first, and
// the made export, declare -x and typeset -x of GOFLAGS handed it on; the
// made read into PATH had the fake found first, and the made export of PATH
// written out left no sdlc to be found. The made `set -k` had the fake found
// in bash; zsh's -k is another option. The made zformat changed PATH in zsh,
// and bash, which has none, ran the call unchanged. Backticks and a quoted
// made word handed GOFLAGS on in both.
func TestAC1AC13ABuiltinReachedThroughAMadeProgramWordIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		// Written out as the review ran it: with `$PWD` in the value, the
		// line is refused today as a git command that cannot be read.
		"$(printf exp%s ort) PATH=/tmp/fb:/usr/bin:/bin; @RUN@",
		"$(printf exp%s ort) GOFLAGS=-v; @RUN@",
		`$(printf set) -k; @RUN@ PATH="$PWD/fake:$PATH"`,
		// The option alone, as `set -k; @RUN@`: a made program given a word
		// that is no name is still a change.
		"$(printf set) -k; @RUN@",
		"$(printf decl%s are) -x GOFLAGS=-v; @RUN@",
		"$(printf type%s set) -x GOFLAGS=-v; @RUN@",
		`$(printf re%s ad) PATH <<< "$PWD/fake:$PATH"; @RUN@`,
		`$(printf zfor%s mat) -f PATH "$PWD/fake:$PATH"; @RUN@`,
		"`printf export` GOFLAGS=-v; @RUN@",
		`"$(printf exp%s ort)" GOFLAGS=-v; @RUN@`,
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-23): the env: drive named alone, before a command that
// writes a name on it, which agentenv.go reads whatever the name; and
// Start-Process given an environment, which hands sdlc the call as a value
// and the setting with it. Not run: no PowerShell was at hand. -Environment
// is PowerShell 7.4's.
func TestAC1AC13APowerShellLineThatSetsANameThroughTheDriveOrStartProcessIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"New-Item -Path Env: -Name GOFLAGS -Value '-run=^$'; @RUN@",
		"ni -Path Env:\\ -Name GOFLAGS -Value '-v'; @RUN@",
		"cd env:; si GOFLAGS '-v'; @RUN@",
		"Set-Location Env:; Set-Item GOFLAGS '-v'; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// Controls: Start-Process given an environment, the review's third line,
	// is refused today, whatever it sets, as a program started without the
	// protected names (agentenv.go's startsAfresh), and the new rule must not
	// take it over by accident.
	for _, command := range []string{
		"Start-Process -FilePath sdlc -ArgumentList 'gate implementation pass' -Environment @{GOFLAGS='-run=^$'} -NoNewWindow -Wait",
		"Start-Process -FilePath sdlc -ArgumentList 'freeze' -Environment @{GOFLAGS='-run=^$'} -NoNewWindow -Wait",
		"saps sdlc 'gate implementation pass' -Environment @{GOFLAGS='-v'} -NoNewWindow -Wait",
		"start sdlc freeze -UseNewEnvironment -NoNewWindow -Wait",
	} {
		refusedAsAChangedEnvironment(t, command, true, us134AgentenvRule)
	}
	// Reading the drive changes nothing, but on PowerShell the call stands
	// alone on its line (security F-30), so a read beside it is refused too.
	for _, command := range us134Rows([]string{"Get-ChildItem Env:; @RUN@", "gci env: | Out-String; @RUN@"}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
}

// AC-1 and AC-13 (F-24): bash's keyword option turned on through shopt, or
// through a set option word the shell makes. In bash 5.3.20 each had the
// stand-in under fake found for a PATH written after the call; zsh has no
// shopt, and its -k is another option, so the rows are refused for the shell
// that takes them. shopt with no -o, or -o with another option, stays allowed.
func TestAC1AC13TheKeywordOptionThroughShoptOrAMadeOptionWordIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		`shopt -os keyword; @RUN@ PATH="$PWD/fake:$PATH"`,
		`shopt -s -o keyword; @RUN@ PATH="$PWD/fake:$PATH"`,
		`set -$(printf '\153'); @RUN@ PATH="$PWD/fake:$PATH"`,
		"shopt -os keyword; @RUN@",
		// allexport the same way: every assignment after it is exported.
		"shopt -os allexport; @RUN@",
		"set -$(printf a); @RUN@",
		`setopt "$(printf allexport)"; @RUN@`,
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range us134Rows([]string{
		"shopt -s extglob; @RUN@",
		"shopt -s nullglob; @RUN@",
		"shopt -o -s pipefail; @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (F-26, the fifth round of design review security): .NET's
// SetEnvironmentVariable reached without its parenthesis straight after the
// method name, by reflection (GetMethod, InvokeMember), by a quoted or a built
// method name, or by the method's Invoke. Each sets GOFLAGS in the process
// that then starts sdlc, as the plain call does. agentenv.go refuses each for
// a protected name, through quotesProtected; that reader is not asked with
// any name, because any quoted word would then refuse (the guards below), so
// the method's name is read in its place, anywhere on a PowerShell line that
// holds the call, in any case, quoted or not, joined or formatted. Not run: no
// PowerShell was at hand. Left out, as the review lists them as trunk-shared
// (F-27): iex or Invoke-Expression given code that builds a setting.
func TestAC1AC13SetEnvironmentVariableReachedByReflectionOrAQuotedNameIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		// The review's three lines; the first with its arguments as a plain
		// array (the review's own spelling is a control below).
		"[Environment].GetMethod('SetEnvironmentVariable', [type[]]@([string],[string])).Invoke($null, ('GOFLAGS','-run=^$')); @RUN@",
		"[Environment]::'SetEnvironmentVariable'('GOFLAGS','-v'); @RUN@",
		"[Environment]::SetEnvironmentVariable.Invoke('GOFLAGS','-v'); @RUN@",
		// The same reached by InvokeMember, by a double-quoted name, and in
		// lower case, as PowerShell takes a type and a member in any case.
		"[Environment].InvokeMember('SetEnvironmentVariable', [Reflection.BindingFlags]'InvokeMethod,Static,Public', $null, $null, @('GOFLAGS','-v')); @RUN@",
		`[System.Environment]::"SetEnvironmentVariable"('GOFLAGS', '-v'); @RUN@`,
		"[environment]::setenvironmentvariable.invoke('GOFLAGS','-v'); @RUN@",
		// A method name joined, formatted or held in a variable.
		"[Environment]::('Set'+'EnvironmentVariable')('GOFLAGS','-v'); @RUN@",
		"[Environment]::('{0}EnvironmentVariable' -f 'Set')('GOFLAGS','-v'); @RUN@",
		"$m = 'SetEnvironmentVariable'; [Environment]::$m('GOFLAGS','-v'); @RUN@",
		"$m = [Environment].GetMethod('Set' + 'EnvironmentVariable', [type[]]([string],[string])); $m.Invoke($null, [object[]]('GOFLAGS','-v')); @RUN@",
		// The plain call with the process scope named.
		"[System.Environment]::SetEnvironmentVariable('GOFLAGS', '-v', 'Process'); @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// Controls: `.Invoke($null, @(…))` after GetMethod is refused today, as
	// sdlc run through a program word the shell makes, and must stay refused
	// under either rule.
	for _, command := range us134Rows([]string{
		"[Environment].GetMethod('SetEnvironmentVariable', [type[]]@([string],[string])).Invoke($null, @('GOFLAGS','-run=^$')); @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true, "review-is-recorded-by-its-reviewer")
	}
	// A quoted word and a string that names a variable change nothing, and
	// were this run's guards. Since security F-30 the call stands alone on a
	// PowerShell line, so each is refused beside it; the reads of
	// [Environment] went the same way with F-29.
	for _, command := range us134Rows([]string{
		"if ($env:GOFLAGS -eq '-v') { 'set' }; @RUN@",
		"Write-Host 'done'; @RUN@",
		"Write-Host 'set GOFLAGS before you run it'; @RUN@",
		"'GOFLAGS' | Out-Null; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// The method's name in the call's own single-quoted note is text sdlc is
	// handed, and stays allowed.
	allowedInEverySession(t, "sdlc gate implementation pass --note 'SetEnvironmentVariable is not called'", true)
}

// AC-1 and AC-13 (F-29, the sixth round of design review security): on a
// PowerShell line that holds the call, any .NET type or member is a change.
// Each round a list of named lookups missed another route to
// SetEnvironmentVariable or to a process started with its own environment,
// so the person chose the class: a type literal, a static member after ::,
// New-Object, Add-Type, a member after a dot, reflection and Invoke, and a
// cast or a type operator (-as, -is), which are type access too. Since F-30
// the call stands alone on the line, which holds all of these and the plain
// cmdlets the guards once allowed beside it. agentenv.go refuses the review's
// lines for a protected name through quotesProtected. A single-quoted string,
// which PowerShell does not run, and the call's own words are not read. Not
// run: no PowerShell was at hand.
func TestAC1AC13APowerShellLineHoldingTheCallThatReachesDotNetIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		// The review's reflection lines: another lookup, a formatted name,
		// then Invoke, and Invoke reached by a quoted member name.
		"[Reflection.RuntimeReflectionExtensions]::GetRuntimeMethod([Environment], ('{0}EnvironmentVariable' -f 'Set'), [type[]]([string],[string])).Invoke($null, ('GOFLAGS','-run=^$')); @RUN@",
		"$ms = [Environment].DeclaredMethods; $ms[0].Invoke($null, ('GOFLAGS','-v')); @RUN@",
		"$m = [Environment].GetMethod('SetEnvironmentVariable', [type[]]([string],[string])); $m.'Invoke'($null, ('GOFLAGS','-v')); @RUN@",
		// The reads of [Environment] the fifth run kept allowed.
		"[Environment]::GetEnvironmentVariable('GOFLAGS'); @RUN@",
		"[Environment]::GetEnvironmentVariable('GOFLAGS', 'Process') | Out-String; @RUN@",
		"[Environment]::ProcessorCount; @RUN@",
		// The class, which no list of names would hold: other types, a
		// type named by a string, New-Object, Add-Type and a script block's
		// Invoke.
		"[IO.File]::ReadAllText('x'); @RUN@",
		"[Math]::Max(1,2); @RUN@",
		"[type]'System.Environment'; @RUN@",
		"New-Object System.Diagnostics.ProcessStartInfo; @RUN@",
		"Add-Type -TypeDefinition 'public static class A { }'; @RUN@",
		"[scriptblock]::Create('Write-Host x').Invoke(); @RUN@",
		// A member after a dot, on a variable and on an expression.
		"$PSVersionTable.PSVersion; @RUN@",
		"(Get-Date).ToString('u'); @RUN@",
		// A cast and the type operators: type access, under the person's
		// wording, and no frozen guard holds one.
		"$x = [string]$env:GOFLAGS; @RUN@",
		"$n = '5' -as [int]; @RUN@",
		"if ($env:GOFLAGS -is [string]) { 'set' }; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// The review's process started through Diagnostics.Process: no call is
	// written as a command, but the line names sdlc and the call's words.
	for _, args := range []string{"'gate implementation pass --note green'", "'freeze'"} {
		for _, start := range []string{"[Diagnostics.Process]::new()", "New-Object System.Diagnostics.Process"} {
			refusedAsAChangedEnvironment(t, "$p = "+start+"; $p.StartInfo.FileName = 'sdlc'; $p.StartInfo.Arguments = "+args+
				"; $p.StartInfo.UseShellExecute = $false; $p.StartInfo.EnvironmentVariables['GOFLAGS'] = '-run=^$'; [void]$p.Start(); $p.WaitForExit()", true)
		}
	}
	// Plain cmdlets beside the call were this run's guards; since security
	// F-30 the call stands alone on a PowerShell line, so they are refused.
	for _, command := range us134Rows([]string{
		"Get-Location; @RUN@",
		"Set-Location internal; @RUN@",
		"Get-ChildItem -Name | Out-Null; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	refusedAsAChangedEnvironment(t, "Write-Host 'done'; sdlc gate implementation pass --note '[IO.File]::ReadAllText'", true)
	// A type named in the call's own single-quoted note is text sdlc is
	// handed, and changes nothing.
	allowedInEverySession(t, "sdlc gate implementation pass --note '[Environment]::GetEnvironmentVariable is read'", true)
}

// AC-1 and AC-13 (F-30, the seventh round of design review security): on
// PowerShell the call stands alone on its line. ForEach-Object calls a
// method by name with no dot on the line, and an Env: path can be built from
// quoted parts, so each reading of a class missed another route; the person
// chose the line rule. A PowerShell line that holds either call is allowed
// only with nothing but whitespace around the call, and in the call's own
// words only literal arguments: a plain or a single-quoted --note. A
// double-quoted note is refused, because PowerShell runs a $(…) inside it,
// and no allowed PowerShell row has one. bash keeps its readings. The
// review's trunk-shared notes have rows too, as the line rule holds them for
// this rule: typographic quotes and dashes (F-31), the Environment provider
// without env: (F-32), and an apostrophe inside a double-quoted string
// (F-33). Not run: no PowerShell was at hand.
func TestAC1AC13OnPowerShellTheCallStandsAloneOnItsLine(t *testing.T) {
	for _, command := range us134Rows([]string{
		// F-30 (i): a method called through ForEach-Object by name, and a
		// single-quoted string run as code the same way.
		"1 | ForEach-Object GetType | ForEach-Object Assembly | ForEach-Object GetType -ArgumentList 'System.Environment' | ForEach-Object GetMethods | Where-Object Name -eq 'SetEnvironmentVariable' | ForEach-Object Invoke -ArgumentList $null, ('GOFLAGS', '-run=^$'); @RUN@",
		"$ExecutionContext | ForEach-Object InvokeCommand | ForEach-Object InvokeScript -ArgumentList '[Environment]::''SetEnvironmentVariable''(''GOFLAGS'', ''-v'')'; @RUN@",
		"Get-Item Env:GOFLAGS | Select-Object -ExpandProperty Value; @RUN@",
		// F-30 (ii): an Env: path built from quoted parts.
		"Set-Item -Path ('En'+'v:'+'GOFLAGS') -Value '-v'; @RUN@",
		// F-31: typographic quotes and dashes, which PowerShell reads as
		// their ASCII forms.
		"[Environment]::SetEnvironmentVariable(‘GOFLAGS’, '-v'); @RUN@",
		"Write-Host 'x’; [Environment]::’SetEnvironmentVariable’(’GOFLAGS’, ’-v’); Write-Host ’y'; @RUN@",
		"Start-Process pwsh –UseNewEnvironment; @RUN@",
		"Start-Process pwsh —UseNewEnvironment; @RUN@",
		// F-32: the Environment provider reached without the text env:.
		"Set-Item Environment::GOFLAGS -Value '-run=^$'; @RUN@",
		`Remove-Item Microsoft.PowerShell.Core\Environment::GOFLAGS; @RUN@`,
		"Push-Location ('En'+'v:'); Remove-Item GOFLAGS; Pop-Location; @RUN@",
		`$d = 'En'+'v:'; Set-Item -Path "${d}GOFLAGS" -Value 'x'; @RUN@`,
		"New-PSDrive -Name E -PSProvider Environment -Root ''; Set-Item E:GOFLAGS '-v'; @RUN@",
		// F-33: an apostrophe inside a double-quoted string opens nothing.
		`Write-Host "it's"; [Environment]::'SetEnvironmentVariable'('GOFLAGS','-v'); Write-Host "x'"; @RUN@`,
		// The class no longer matters: anything beside the call, before it
		// or after it.
		"Get-Date; @RUN@",
		"Write-Output 1; @RUN@",
		"Get-ChildItem -Name | ForEach-Object { Write-Host $_ }; @RUN@",
		"@RUN@; Write-Host 'x'",
		"@RUN@ | Out-Null",
		"@RUN@ # done",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// A double-quoted note: PowerShell expands it before sdlc starts.
	for _, command := range []string{
		`sdlc gate implementation pass --note "green"`,
		`sdlc gate implementation pass --note "$(Set-Item Env:GOFLAGS '-v')"`,
	} {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// The call alone, with whitespace around it, and with a plain or a
	// single-quoted note, whatever the note names.
	for _, command := range us134Rows([]string{"@RUN@", "  @RUN@  ", "\t@RUN@\t"}) {
		allowedInEverySession(t, command, true)
	}
	for _, command := range []string{
		"sdlc gate implementation pass --note green",
		"sdlc gate implementation pass --note 'green, and Write-Host done'",
		"sdlc gate implementation pass --note '[Environment]::SetEnvironmentVariable is named, not called'",
	} {
		allowedInEverySession(t, command, true)
	}
	// bash keeps its readings: the same plain commands beside the call stay
	// allowed there.
	for _, command := range us134Rows([]string{"cd internal; @RUN@", "echo done; @RUN@", "@RUN@; echo done"}) {
		allowedInEverySession(t, command, false)
	}
}

// us134Encoded is a script as pwsh's -EncodedCommand takes it: the base64 of
// its UTF-16LE text.
func us134Encoded(script string) string {
	var b []byte
	for _, u := range utf16.Encode([]rune(script)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// AC-1 and AC-13 (security F-35, F-36 and F-37): on PowerShell, a line that
// may run the call must be the call alone. Each line below runs sdlc freeze
// or the implementation gate without the call written as plain words:
// through a parenthesised operand or program, which the reader splits off;
// a variable or the call operator; Start-Process or a .NET Process given its
// arguments before sdlc's name, or sdlc's name built inside typographic
// quotes; Invoke-Expression; and pwsh given an encoded command. Each is
// asked bare and after a setting, and each is refused for what runs it, not
// for naming sdlc in front of the call's words. A computed program is read
// as the call where the words after it may be the call's; one given other
// words stays allowed, because the US-129 corpus pins the plugin's own
// launcher, `& "$env:…\bin\sdlc-hook.cmd"`, as allowed on PowerShell. bash
// keeps its readings. Not run: no PowerShell was at hand.
func TestAC1AC13OnPowerShellALineThatMayRunTheCallMustBeTheCallAlone(t *testing.T) {
	settings := []string{"", "$env:GOFLAGS='-run=^$'; ", "Set-Item Env:GOFLAGS '-v'; "}
	var lines []string
	for _, call := range us134Calls {
		args := strings.TrimPrefix(call, "@SDLC@ ")
		lines = append(lines,
			// A program given by a variable or through the call operator.
			"$s = 'sdlc'; & $s "+args,
			"$s = 'sdlc'; $s "+args,
			"& (gcm sdl?) "+args,
			"& ('{0}lc' -f 'sd') "+args,
			"& sdlc "+args,
			// Invoke-Expression given the call as text, written or built.
			"Invoke-Expression 'sdlc "+args+"'",
			"iex 'sdlc "+args+"'",
			"iex ('sd'+'lc "+args+"')",
			"iex ('{0} {1}' -f 'sdlc', '"+args+"')",
			// Start-Process given its arguments before sdlc's name, and after.
			"Start-Process -ArgumentList '"+args+"' -FilePath sdlc -NoNewWindow -Wait",
			"Start-Process -FilePath sdlc -ArgumentList '"+args+"' -NoNewWindow -Wait",
			"saps -ArgumentList '"+args+"' -FilePath ('sd'+'lc') -NoNewWindow -Wait",
			// pwsh given an encoded command that sets GOFLAGS and runs the call.
			"pwsh -EncodedCommand "+us134Encoded("$env:GOFLAGS='-run=^$'; sdlc "+args),
			"powershell -enc "+us134Encoded("$env:GOFLAGS='-v'; sdlc "+args),
			"pwsh -ec "+us134Encoded("sdlc "+args),
		)
		// A .NET Process given its arguments before its file name, and with
		// the name built by + inside typographic quotes.
		for _, name := range []string{"'sdlc'", "‘sd’+‘lc’"} {
			lines = append(lines, "$p = [Diagnostics.Process]::new(); $p.StartInfo.Arguments = '"+args+"'; $p.StartInfo.FileName = "+name+
				"; $p.StartInfo.UseShellExecute = $false; $p.StartInfo.EnvironmentVariables['GOFLAGS'] = '-run=^$'; [void]$p.Start(); $p.WaitForExit()")
		}
	}
	// F-35: a parenthesised operand of sdlc, in the freeze and in each word
	// of the gate.
	lines = append(lines,
		"sdlc ('{0}eze' -f 'fre')",
		"sdlc gate ('{0}ation' -f 'implement') pass --note green",
		"sdlc gate implementation ('{0}ss' -f 'pa') --note green",
		"sdlc ('gate') implementation pass --note green",
	)
	for _, line := range lines {
		for _, setting := range settings {
			refusedAsAChangedEnvironment(t, setting+line, true)
		}
	}
	// Invoke-Expression given a variable or piped text: the line names the
	// call in the text it hands over.
	for _, call := range us134Calls {
		args := strings.TrimPrefix(call, "@SDLC@ ")
		for _, setting := range settings {
			refusedAsAChangedEnvironment(t, setting+"$c = 'sdlc "+args+"'; iex $c", true)
			refusedAsAChangedEnvironment(t, setting+"'sdlc "+args+"' | Invoke-Expression", true)
		}
	}
	// A computed program is the call where the words after it may be the
	// call's: made words, or freeze, or gate and what may follow it.
	for _, command := range us134Rows([]string{"& $exe @ARGS@", "$exe = 'sdlc'; & $exe @ARGS@"}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// Control: a made program given a splat is refused today, as sdlc run
	// through a program word the shell makes, and stays refused.
	refusedAsAChangedEnvironment(t, "& $exe @a", true, "review-is-recorded-by-its-reviewer")
	// A computed program whose words cannot be the call is not this rule's:
	// the US-129 corpus holds the plugin's own launcher, a program built from
	// a variable, as allowed on PowerShell, and the rule keeps it so.
	allowedInEverySession(t, "& $exe build", true)
	allowedInEverySession(t, "$exe = 'go'; & $exe build ./...", true)
	allowedInEverySession(t, "& (gcm go) vet ./...", true)
	// What the guards keep: sdlc's other commands beside a setting, and bash.
	allowedInEverySession(t, "$env:GOFLAGS = '-v'; sdlc status", true)
	allowedInEverySession(t, "$env:GOFLAGS = '-v'", true)
	for _, command := range []string{"$exe build", `"$GO" build ./...`} {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (architect F-13): on PowerShell the call may follow one
// prefix, so that a session run from the main checkout can run it in the
// story's worktree, as sdlc takes no directory flag. The prefix is
// Set-Location or cd, case aside, given an absolute filesystem path as a
// plain word or a whole single-quoted string, then ; or &&. sl, chdir,
// Push-Location and pushd are refused: the runbook writes cd, and each
// alias more is a spelling more to read for nothing. Anything else in the
// prefix, or after it, is refused. Not run: no PowerShell was at hand.
func TestAC1AC13OnPowerShellTheCallMayFollowASetLocationToAnAbsolutePath(t *testing.T) {
	for _, command := range us134Rows([]string{
		"Set-Location '/Users/dev/sdlc/.sdlc/worktrees/US-134'; @RUN@",
		"Set-Location /Users/dev/sdlc/.sdlc/worktrees/US-134 && @RUN@",
		"Set-Location 'C:\\Users\\dev\\My Work\\US-134'; @RUN@",
		"Set-Location C:\\Users\\dev\\US-134 && @RUN@",
		"Set-Location D:/work/US-134; @RUN@",
		"cd '/Users/dev/sdlc/.sdlc/worktrees/US-134'; @RUN@",
		"cd C:\\Users\\dev\\US-134 && @RUN@",
		"set-location '/tmp/US-134';@RUN@",
		"CD /tmp/US-134 &&@RUN@",
		"  cd '/tmp/US-134' ; @RUN@  ",
	}) {
		allowedInEverySession(t, command, true)
	}
	allowedInEverySession(t, "cd '/tmp/US-134'; sdlc gate implementation pass --note 'green, in the worktree'", true)
	for _, command := range us134Rows([]string{
		// A path that is not absolute, or that the line computes.
		"Set-Location internal; @RUN@",
		"Set-Location ./internal; @RUN@",
		"Set-Location ~; @RUN@",
		`Set-Location "C:\work\US-134"; @RUN@`,
		"Set-Location $wt; @RUN@",
		"Set-Location $(Get-Location); @RUN@",
		"Set-Location ('/tmp/' + 'US-134'); @RUN@",
		"Set-Location ‘/tmp/US-134’; @RUN@",
		// A provider path, not the file system.
		"Set-Location Env:; @RUN@",
		"Set-Location 'Env:\\'; @RUN@",
		"cd Environment::; @RUN@",
		// Two prefixes, or something else beside the prefix.
		"Set-Location '/a'; Set-Location '/b'; @RUN@",
		"Set-Location '/a'; Write-Host x; @RUN@",
		"$env:GOFLAGS='-v'; Set-Location '/a'; @RUN@",
		"Set-Location '/a'; @RUN@; Write-Host x",
		"Set-Location '/a' | Out-Null; @RUN@",
		"Set-Location -Path '/a'; @RUN@",
		"Set-Location '/a' || @RUN@",
		// Another command for the directory.
		"Push-Location '/a'; @RUN@",
		"pushd '/a'; @RUN@",
		"sl '/a'; @RUN@",
		"chdir '/a'; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// bash keeps its readings: cd with a relative path before the call stays
	// allowed there.
	for _, command := range us134Rows([]string{"cd internal && @RUN@", "cd '/tmp/US-134' && @RUN@"}) {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (security F-39, F-40 and F-41, the ninth round): on
// PowerShell, a line that names sdlc and the call's words, in any order and
// however quoted or joined, may be the call, and must be the call alone.
// Literal text naming the call can be run in more ways than any list holds:
// a script block made from it, an alias for iex, a job, a runspace, a .NET
// process under a namespace. A statement nested in a block, a parenthesis,
// a subexpression, an array or a double-quoted string's $( ) is read as the
// line's own, at every depth. pwsh takes its encoded-command switch after
// one or two dashes of any kind, or a slash. Each row is asked bare and after
// a setting. A commit message that names the call is refused too, as the
// person's wording reads every line; its route is git commit -F. bash keeps
// its readings. Not run: no PowerShell was at hand.
func TestAC1AC13OnPowerShellALineThatNamesOrNestsTheCallMustBeTheCallAlone(t *testing.T) {
	settings := []string{"", "$env:GOFLAGS='-run=^$'; ", "Set-Item Env:GOFLAGS '-v'; "}
	var lines []string
	for _, call := range us134Calls {
		args := strings.TrimPrefix(call, "@SDLC@ ")
		lines = append(lines,
			// F-39: literal text naming the call, run another way.
			"[scriptblock]::Create('sdlc "+args+"').Invoke()",
			"& ([scriptblock]::Create('sdlc "+args+"'))",
			"$ExecutionContext.InvokeCommand.InvokeScript('sdlc "+args+"')",
			"& (gcm iex) 'sdlc "+args+"'",
			"Set-Alias x iex; x 'sdlc "+args+"'",
			"Invoke-Command -ScriptBlock ([scriptblock]::Create('sdlc "+args+"'))",
			"Start-Job -ScriptBlock ([scriptblock]::Create('sdlc "+args+"')) | Receive-Job -Wait",
			"using namespace System.Diagnostics; [Process]::Start('sdlc', '"+args+"')",
			// Invokers no list names, with the call's words before sdlc's.
			"[PowerShell]::Create().AddScript('sdlc "+args+"').Invoke()",
			"$w = '"+args+"'; $n = 'sd' + 'lc'; [PowerShell]::Create().AddScript(\"$n $w\").Invoke()",
			"$a = '"+args+"'; $n = 'sdlc'; Register-EngineEvent -SourceIdentifier x -Action ([scriptblock]::Create($n + ' ' + $a))",
			// F-40: the call operator and Start-Process nested, at depth one
			// and two, in a block, an array, a subexpression and a
			// double-quoted string or here-string.
			"if ($true) { & (gcm sdl?) "+args+" }",
			"if ($true) { if ($true) { & (gcm sdl?) "+args+" } }",
			"& { & ('{0}lc' -f 'sd') "+args+" }",
			"$r = @(& (gcm sdl?) "+args+")",
			"$r = $(& ('{0}lc' -f 'sd') "+args+")",
			"Write-Host \"$(& (gcm sdl?) "+args+")\"",
			"Write-Host \"a $(if ($true) { & (gcm sdl?) "+args+" }) b\"",
			"Write-Output @\"\n$(& (gcm sdl?) "+args+")\n\"@",
			"if ($true) { Start-Process -ArgumentList '"+args+"' -FilePath sdlc -NoNewWindow -Wait }",
			// F-41: the encoded-command switch after other dashes, or a slash.
			"pwsh --ec "+us134Encoded("sdlc "+args),
			"pwsh –ec "+us134Encoded("sdlc "+args),
			"pwsh —EncodedCommand "+us134Encoded("sdlc "+args),
			"powershell /enc "+us134Encoded("sdlc "+args),
			"pwsh --EnCoDeD "+us134Encoded("sdlc "+args),
		)
	}
	lines = append(lines,
		// F-40: a parenthesised operand inside a block.
		"1 | ForEach-Object { sdlc ('{0}eze' -f 'fre') }",
		"1 | ForEach-Object { sdlc gate ('{0}ation' -f 'implement') pass --note green }",
		// A commit message that names the call.
		"git commit -m 'sdlc freeze docs'",
		"git commit -m 'run sdlc gate implementation pass first'",
	)
	for _, line := range lines {
		for _, setting := range settings {
			refusedAsAChangedEnvironment(t, setting+line, true)
		}
	}
	// What stays allowed: the call alone, with a note that names the call;
	// the prefix; a line naming sdlc but not the call, or the call's words but
	// not sdlc, for which .sdlc as a directory is not sdlc's name.
	for _, command := range []string{
		"sdlc gate implementation pass --note 'sdlc freeze ran first'",
		"Set-Location '/Users/dev/sdlc/.sdlc/worktrees/US-134'; sdlc freeze",
		"$env:GOFLAGS = '-v'; sdlc review list",
		"Write-Host 'freeze'; Write-Host done",
		"Get-Content .sdlc/config.json; Write-Host 'freeze'",
		"& $exe build",
	} {
		allowedInEverySession(t, command, true)
	}
	// bash keeps its readings: the commit message stays allowed there.
	for _, command := range []string{"git commit -m 'sdlc freeze docs'", "git commit -m 'run sdlc gate implementation pass first'"} {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (security F-43 and F-44, the tenth round): on PowerShell,
// pwsh or powershell given anything but literal words may be the call, and
// so may any line holding a typographic quote. pwsh takes its switch and its
// script from a splat, a variable, a subexpression or an expression that
// decodes base64 as readily as from a word, so only plain words and
// single-quoted strings are read as what they say. PowerShell ends a string
// at U+2018 to U+201E as at an ASCII quote, which the reader does not, so a
// statement between typographic quotes reads as text; a line holding one is
// read as maybe the call, whatever it holds. Each row is asked bare and after
// a setting, with each dash pwsh takes. Not run: no PowerShell was at hand.
func TestAC1AC13OnPowerShellAShellGivenMoreThanWordsOrATypographicQuoteMayBeTheCall(t *testing.T) {
	settings := []string{"", "$env:GOFLAGS='-run=^$'; ", "Set-Item Env:GOFLAGS '-v'; "}
	var lines []string
	for _, call := range us134Calls {
		args := strings.TrimPrefix(call, "@SDLC@ ")
		enc := us134Encoded("$env:GOFLAGS='-v'; sdlc " + args)
		dec := "[Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('" + enc + "'))"
		// F-43: the switch or the script reaching the shell as a splat, a
		// variable, a subexpression or a decoding expression.
		for _, sh := range []string{"pwsh", "powershell"} {
			for _, d := range []string{"-", "--", "–", "—", "/"} {
				lines = append(lines,
					"$a = '"+d+"ec', '"+enc+"'; "+sh+" @a",
					"$sw = '"+d+"e' + 'c'; "+sh+" $sw "+enc,
					sh+" "+d+"ec $('"+enc+"')",
					sh+" "+d+"c ("+dec+")",
					"$s = "+dec+"; "+sh+" "+d+"Command $s",
				)
			}
		}
		lines = append(lines,
			"$s = "+dec+"; pwsh.exe -c \"$s\"",
			"$s = "+dec+"; /usr/bin/pwsh -NoProfile -Command $s",
			"$e = '"+enc+"'; POWERSHELL.EXE -EncodedCommand $e",
		)
		// F-44: each typographic quote, single and double, closing a string
		// early around a statement, around the call's words, and around part
		// of sdlc's name.
		for _, q := range []string{"‘", "’", "‚", "‛"} {
			lines = append(lines, "Write-Host 'x"+q+"; & (gcm sdl?) "+args+"; Write-Host "+q+"y'")
		}
		for _, q := range []string{"“", "”", "„"} {
			lines = append(lines, "Write-Host \"x"+q+"; & (gcm sdl?) "+args+"; Write-Host "+q+"y\"")
		}
		for _, q := range []string{"‘", "’", "‚", "‛", "“", "”", "„"} {
			lines = append(lines,
				"& "+q+"sdlc"+q+" "+args,
				"sdl"+q+"c"+q+" "+args,
			)
		}
	}
	lines = append(lines,
		"sdlc fr’ee’ze",
		"sdlc ‘gate’ implementation pass --note green",
		"sdlc gate implementation pass --note “green”",
	)
	for _, line := range lines {
		for _, setting := range settings {
			refusedAsAChangedEnvironment(t, setting+line, true)
		}
	}
	// A line holding a typographic quote is maybe the call whatever it holds,
	// and pwsh given a variable is too, though neither names sdlc.
	for _, command := range []string{
		"Write-Host 'it’s fine'",
		"Write-Host “done”",
		"$script = './build.ps1'; pwsh -File $script",
	} {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// What stays allowed: the call alone, pwsh given only literal words, and a
	// line with no typographic quote that does not name the call.
	for _, command := range []string{
		"sdlc freeze",
		"sdlc gate implementation pass --note 'green, it''s done'",
		"pwsh -NoProfile -File ./build.ps1",
		"pwsh -NoProfile -Command 'Get-Date'",
		"powershell -ExecutionPolicy Bypass -File C:\\Users\\dev\\build.ps1",
		"$env:GOFLAGS = '-v'; pwsh -NoProfile -File ./build.ps1",
		"Write-Host 'its fine'",
	} {
		allowedInEverySession(t, command, true)
	}
	// bash keeps its readings: a typographic quote there is a character.
	allowedInEverySession(t, "echo 'it’s fine'", false)
}
