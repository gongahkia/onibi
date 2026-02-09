import XCTest
@testable import Onibi

final class GhosttyConfigParserTests: XCTestCase {
    
    // MARK: - Key=Value Parsing Tests
    
    func testParseKeyValueWithEquals() {
        let contents = """
        theme = nord
        font_family = JetBrains Mono
        font_size = 14
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.theme, "nord")
        XCTAssertEqual(config.fontFamily, "JetBrains Mono")
        XCTAssertEqual(config.fontSize, 14)
    }
    
    func testParseKeyValueWithSpaces() {
        let contents = """
        theme nord
        font_family Cascadia Code
        font_size 16
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.theme, "nord")
        XCTAssertEqual(config.fontFamily, "Cascadia Code")
        XCTAssertEqual(config.fontSize, 16)
    }
    
    func testParseMixedFormat() {
        let contents = """
        theme = dracula
        font_family Menlo
        background #1e1e1e
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.theme, "dracula")
        XCTAssertEqual(config.fontFamily, "Menlo")
        XCTAssertEqual(config.backgroundColor, "#1e1e1e")
    }
    
    // MARK: - Key-Value Parsing Tests
    
    func testParseBackgroundColor() {
        let contents = "background = #282c34"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.backgroundColor, "#282c34")
    }
    
    func testParseForegroundColor() {
        let contents = "foreground #abb2bf"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.foregroundColor, "#abb2bf")
    }
    
    func testParseCursorColor() {
        let contents = "cursor_color = #528bff"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.cursorColor, "#528bff")
    }
    
    func testParseSelectionBackground() {
        let contents = "selection_background #3e4451"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.selectionBackground, "#3e4451")
    }
    
    func testParseBooleanTrue() {
        let contents = """
        window_decorations = true
        shell_integration = zsh-integration
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.windowDecorations, true)
        XCTAssertEqual(config.shellIntegration, true)
    }
    
    func testParseBooleanFalse() {
        let contents = """
        window_decorations false
        shell_integration = none
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.windowDecorations, false)
        XCTAssertEqual(config.shellIntegration, false)
    }
    
    func testParseBooleanNumeric() {
        let contents = "window_decorations = 1"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.windowDecorations, true)
    }
    
    func testParseCustomProperties() {
        let contents = """
        custom_key = custom_value
        another_setting something
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.customProperties["custom_key"], "custom_value")
        XCTAssertEqual(config.customProperties["another_setting"], "something")
    }
    
    // MARK: - Color Parsing Tests
    
    func testParseColorWithHash() {
        let color = OnibiConfigParser.parseColor("#ff0000")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 255)
        XCTAssertEqual(color?.g, 0)
        XCTAssertEqual(color?.b, 0)
    }
    
    func testParseColorWithoutHash() {
        let color = OnibiConfigParser.parseColor("00ff00")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 0)
        XCTAssertEqual(color?.g, 255)
        XCTAssertEqual(color?.b, 0)
    }
    
    func testParseColorBlue() {
        let color = OnibiConfigParser.parseColor("#0000ff")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 0)
        XCTAssertEqual(color?.g, 0)
        XCTAssertEqual(color?.b, 255)
    }
    
    func testParseColorMixed() {
        let color = OnibiConfigParser.parseColor("#ab12cd")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 171) // 0xAB
        XCTAssertEqual(color?.g, 18)  // 0x12
        XCTAssertEqual(color?.b, 205) // 0xCD
    }
    
    func testParseColorWithWhitespace() {
        let color = OnibiConfigParser.parseColor("  #ffffff  ")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 255)
        XCTAssertEqual(color?.g, 255)
        XCTAssertEqual(color?.b, 255)
    }
    
    func testParseColorBlack() {
        let color = OnibiConfigParser.parseColor("#000000")
        
        XCTAssertNotNil(color)
        XCTAssertEqual(color?.r, 0)
        XCTAssertEqual(color?.g, 0)
        XCTAssertEqual(color?.b, 0)
    }
    
    // MARK: - Invalid Input Tests
    
    func testParseInvalidColorLength() {
        let color = OnibiConfigParser.parseColor("#fff")
        
        XCTAssertNil(color)
    }
    
    func testParseInvalidColorFormat() {
        let color = OnibiConfigParser.parseColor("not-a-color")
        
        XCTAssertNil(color)
    }
    
    func testParseEmptyContent() {
        let config = OnibiConfigParser.parse(contents: "")
        
        XCTAssertNil(config.theme)
        XCTAssertNil(config.fontFamily)
        XCTAssertNil(config.fontSize)
    }
    
    func testParseCommentsIgnored() {
        let contents = """
        # This is a comment
        theme = nord
        # Another comment
        font_size = 14
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.theme, "nord")
        XCTAssertEqual(config.fontSize, 14)
    }
    
    func testParseEmptyLinesIgnored() {
        let contents = """
        theme = nord
        
        font_size = 14
        
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.theme, "nord")
        XCTAssertEqual(config.fontSize, 14)
    }
    
    func testParseInvalidFontSize() {
        let contents = """
        font_size = invalid
        font_family = Mono
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertNil(config.fontSize)
        XCTAssertEqual(config.fontFamily, "Mono")
    }
    
    func testParseNegativeFontSize() {
        let contents = "font_size = -10"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertNil(config.fontSize)
    }
    
    func testParseZeroFontSize() {
        let contents = "font_size = 0"
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertNil(config.fontSize)
    }
    
    func testParseMalformedLine() {
        let contents = """
        valid_key = valid_value
        malformed
        another_key = another_value
        """
        
        let config = OnibiConfigParser.parse(contents: contents)
        
        XCTAssertEqual(config.customProperties["valid_key"], "valid_value")
        XCTAssertEqual(config.customProperties["another_key"], "another_value")
    }
    
    // MARK: - Dictionary Parsing Tests
    
    func testParseDictionary() {
        let dictionary: [String: String] = [
            "theme": "gruvbox",
            "font_family": "Fira Code",
            "font_size": "12",
            "background": "#282828"
        ]
        
        let config = OnibiConfigParser.parse(dictionary: dictionary)
        
        XCTAssertEqual(config.theme, "gruvbox")
        XCTAssertEqual(config.fontFamily, "Fira Code")
        XCTAssertEqual(config.fontSize, 12)
        XCTAssertEqual(config.backgroundColor, "#282828")
    }
    
    func testParseDictionaryNormalizesKeys() {
        let dictionary: [String: String] = [
            "Font-Family": "Monaco",
            "THEME": "solarized"
        ]
        
        let config = OnibiConfigParser.parse(dictionary: dictionary)
        
        XCTAssertEqual(config.fontFamily, "Monaco")
        XCTAssertEqual(config.theme, "solarized")
    }
    
    // MARK: - Extension Tests
    
    func testBackgroundColorRGB() {
        let contents = "background = #ff0000"
        let config = OnibiConfigParser.parse(contents: contents)
        
        let rgb = config.backgroundColorRGB
        
        XCTAssertNotNil(rgb)
        XCTAssertEqual(rgb?.r, 255)
        XCTAssertEqual(rgb?.g, 0)
        XCTAssertEqual(rgb?.b, 0)
    }
    
    func testForegroundColorRGB() {
        let contents = "foreground = #00ff00"
        let config = OnibiConfigParser.parse(contents: contents)
        
        let rgb = config.foregroundColorRGB
        
        XCTAssertNotNil(rgb)
        XCTAssertEqual(rgb?.r, 0)
        XCTAssertEqual(rgb?.g, 255)
        XCTAssertEqual(rgb?.b, 0)
    }
    
    func testColorRGBWithoutValue() {
        let config = OnibiConfigParser.Config()
        
        XCTAssertNil(config.backgroundColorRGB)
        XCTAssertNil(config.foregroundColorRGB)
    }
}
