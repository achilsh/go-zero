package httprewrite

import (
	"regexp"
	"strings"
)

// RewriteRule 表示一个URL重写规则
type RewriteRule struct {
	Method     string         // HTTP方法，*表示所有方法
	Pattern    string         // 匹配模式，可以是精确路径或正则表达式
	Target     string         // 目标路径
	StatusCode int            // 0表示内部重写，3xx表示重定向； default: 0
	isRegex    bool           // 是否为正则表达式规则
	regex      *regexp.Regexp // 编译后的正则表达式
}

// RegexpRouter 处理URL重写规则
type RegexpRouter struct {
	//first key: method, second key: pattern
	exactRules map[string]map[string]RewriteRule // 精确匹配规则: method -> path -> rule
	//first key: method
	regexRules map[string][]RewriteRule // 正则匹配规则: method -> rules
}

// NewRegexpRouter 创建一个新的RegexpRouter
func NewRegexpRouter() *RegexpRouter {
	return &RegexpRouter{
		exactRules: make(map[string]map[string]RewriteRule),
		regexRules: make(map[string][]RewriteRule),
	}
}

// AddRule 添加一个重写规则
// method: HTTP方法，*表示所有方法
// pattern: 匹配模式，如果包含正则元字符则视为正则表达式
// target: 目标路径
// statusCode: 0表示内部重写，3xx表示重定向
func (r *RegexpRouter) AddRule(method, pattern, target string, statusCode int) error {
	// 标准化方法名
	method = strings.ToUpper(method)
	if method == "" {
		method = "*"
	}

	// 判断是否为正则表达式
	isRegex := isRegexPattern(pattern)

	var regex *regexp.Regexp
	var err error
	if isRegex {
		regex, err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
	}

	rule := RewriteRule{
		Method:     method,
		Pattern:    pattern,
		Target:     target,
		StatusCode: statusCode,
		isRegex:    isRegex,
		regex:      regex,
	}

	// 根据是否为正则表达式添加到不同的规则集合
	if !isRegex {
		// 精确匹配规则
		if _, exists := r.exactRules[method]; !exists {
			r.exactRules[method] = make(map[string]RewriteRule)
		}
		r.exactRules[method][pattern] = rule
	} else {
		// 正则表达式规则
		if _, exists := r.regexRules[method]; !exists {
			r.regexRules[method] = []RewriteRule{}
		}
		r.regexRules[method] = append(r.regexRules[method], rule)
	}

	return nil
}

// 判断一个模式是否为正则表达式
func isRegexPattern(pattern string) bool {
	// 检查是否包含常见的正则表达式元字符
	metaChars := `\.+*?()|[]{}^$`
	for _, c := range metaChars {
		if strings.ContainsRune(pattern, c) {
			return true
		}
	}
	return false
}

// first param is method, eg: post, get， req.Method; second param is path, eg: req.URL.Path
//
//	return new path value
func (r *RegexpRouter) RewriteUrl(method string, path string) string {
	if r == nil {
		return path
	}
	if len(method) <= 0 || len(path) <= 0 {
		return path
	}
	method = strings.ToUpper(method)

	//  精准匹配
	if rules, ok := r.exactRules[method]; ok {
		if rule, exists := rules[path]; exists {
			return r.ApplyRule(rule, path)
		}
	}

	// 2. 尝试精确匹配"*"方法的规则
	if rules, ok := r.exactRules["*"]; ok {
		if rule, exists := rules[path]; exists {
			return r.ApplyRule(rule, path)

		}
	}

	// 3. 尝试正则匹配当前方法的规则
	if rules, ok := r.regexRules[method]; ok {
		for _, rule := range rules {
			if rule.regex.MatchString(path) {
				return r.ApplyRule(rule, path)
			}
		}
	}

	// 4. 尝试正则匹配"*"方法的规则
	if rules, ok := r.regexRules["*"]; ok {
		for _, rule := range rules {
			if rule.regex.MatchString(path) {
				return r.ApplyRule(rule, path)
			}
		}
	}

	return path
}

// second param is req.URL.Path; return is new path
func (r *RegexpRouter) ApplyRule(rule RewriteRule, path string) string {
	var newPath string
	if rule.isRegex {
		newPath = rule.regex.ReplaceAllString(path, rule.Target)
		return newPath
	} else {
		newPath = rule.Target
		return newPath
	}
	return path
}
