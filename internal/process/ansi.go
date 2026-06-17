package process

// ansiParser implements a tiny ANSI code parser. This is for the purposes of
// telling whether or not we're in the middle of an ANSI sequence before
// inserting data of our own.
// A reasonable summary of ANSI escape codes is at Wikipedia
// https://en.wikipedia.org/wiki/ANSI_escape_code
// including mention of some of the necessary deviations from the standards
// (such as allowing some sequences to terminate with BEL instead of ESC '\').
type ansiParser struct {
	state *ansiParserState
}

// Write passes more bytes through the parser.
func (m *ansiParser) Write(data []byte) (int, error) {
	for _, b := range data {
		if m.state != nil {
			m.state = m.state[b]
			continue
		}
		if b == 0x1b {
			m.state = initialANSIState
		}
	}
	return len(data), nil
}

// insideCode reports if the data is in the middle of an ANSI sequence.
// The parser is mid-sequence if it's in any state other than nil ("normal").
func (m *ansiParser) insideCode() bool { return m.state != nil }

// ansiParserState is a possible state of the parser. It's a map of incoming-
// byte to next-state. Most next-states are nil (they exit the escape code).
type ansiParserState [256]*ansiParserState

var (
	// initialANSIState is the state the parser enters once it reads ESC.
	initialANSIState = &ansiParserState{
		// Note that most bytes immediately following ESC terminate the sequence.
		// The following require more processing:
		'[': csiParameterState, // CSI
		']': stTextState,       // OSC
		'P': stTextState,       // DCS
		'X': stTextState,       // SOS
		'^': stTextState,       // PM
		'_': stTextState,       // APC
	}
	// csiParameter state is the state the parser is in after ESC '['
	csiParameterState = &ansiParserState{}

	// stTextState is one of the ST-terminated text states (OSC, DCS, APC, etc)
	stTextState = &ansiParserState{}
)

// The "looping states" can't be built as struct literals since they refer to
// themselves, so they are constructed in init.
func init() {
	// Control Sequence Introducers (CSI):
	//   ESC '[' [0-9:;<=>?]* [ !"#$%&'()*+,-./]* [@A–Z[\]^_`a–z{|}~]
	// or...
	//   ESC '[' (0x30-0x3F)* (0x20-0x2F)* (0x40-0x7E)
	// or...
	//   ESC '[' (parameter byte)* (intermediate byte)* (final byte)
	// So the "parameter bytes" and "intermediate bytes" need to loop,
	// and any other byte is the "final byte" which terminates the sequence.
	// Here's an ASCII-art state diagram:
	//
	//         0x30-0x3F          0x20-0x2F
	//             ^|                ^|
	//             |v                |v
	// () --'['--> () --0x20-0x2F--> () --anything else--> (nil)
	//             |
	//             +--anything else--> (nil)
	//
	csiIntermediate := &ansiParserState{}
	for b := byte(0x30); b <= 0x3F; b++ {
		csiParameterState[b] = csiParameterState
	}
	for b := byte(0x20); b <= 0x2F; b++ {
		csiParameterState[b] = csiIntermediate
		csiIntermediate[b] = csiIntermediate
	}

	// OSC, APC, DCS, SOS, PM have the form:
	//   ESC [PX]^_] (~arbitrary text) (BEL | ESC '\')
	// So all bytes need to loop except BEL and ESC \ (that's String Terminator)
	// which terminate the sequence.
	//
	//            not BEL or ESC
	//                 ^|
	//                 |v
	// () --[PX]^_]--> () <--not '\'--+
	//                  |             |
	//                  +----ESC----> () --'\'--> (nil)
	//                  |
	//                  +--BEL--> (nil)
	//
	stEscapeState := &ansiParserState{}
	for b := range 256 {
		stTextState[byte(b)] = stTextState
		stEscapeState[byte(b)] = stTextState
	}
	stTextState[0x07] = nil
	stTextState[0x1b] = stEscapeState
	stEscapeState['\\'] = nil
}
