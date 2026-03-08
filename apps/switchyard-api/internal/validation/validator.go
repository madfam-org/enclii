package validation

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

var (
	// DNS name validation (RFC 1123)
	dnsNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)

	// Environment variable name validation
	envVarRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

	// Git repository URL validation
	gitRepoRegex = regexp.MustCompile(`^(https?://|git@)[\w\.\-]+[:/][\w\.\-]+/[\w\.\-]+\.git$`)

	// Kubernetes namespace validation
	k8sNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

type Validator struct {
	validate *validator.Validate
}

func NewValidator() *Validator {
	validate := validator.New()

	// Register custom validators (errors indicate programming bugs, not runtime failures)
	_ = validate.RegisterValidation("dnsname", validateDNSName)
	_ = validate.RegisterValidation("envvar", validateEnvVarName)
	_ = validate.RegisterValidation("gitrepo", validateGitRepo)
	_ = validate.RegisterValidation("k8sname", validateK8sName)
	_ = validate.RegisterValidation("project_slug", validateProjectSlug)
	_ = validate.RegisterValidation("service_name", validateServiceName)
	_ = validate.RegisterValidation("safe_string", validateSafeString)
	_ = validate.RegisterValidation("port_number", validatePortNumber)

	return &Validator{validate: validate}
}

type ValidationError struct {
	Field   string `json:"field"`
	Tag     string `json:"tag"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	var messages []string
	for _, err := range v {
		messages = append(messages, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}
	return strings.Join(messages, "; ")
}

// Request validation structs
type CreateProjectRequest struct {
	Name string `json:"name" validate:"required,min=1,max=63,safe_string"`
	Slug string `json:"slug" validate:"required,project_slug"`
}

type CreateServiceRequest struct {
	Name        string      `json:"name" validate:"required,service_name"`
	GitRepo     string      `json:"git_repo" validate:"required,gitrepo"`
	BuildConfig BuildConfig `json:"build_config"`
}

type BuildConfig struct {
	Type       string `json:"type" validate:"required,oneof=auto dockerfile buildpack"`
	Dockerfile string `json:"dockerfile,omitempty" validate:"omitempty,safe_string,max=255"`
	Buildpack  string `json:"buildpack,omitempty" validate:"omitempty,safe_string,max=255"`
}

type DeployRequest struct {
	Environment string `json:"environment" validate:"required,oneof=dev staging prod"`
	ReleaseID   string `json:"release_id,omitempty" validate:"omitempty,uuid"`
	Wait        bool   `json:"wait"`
}

type CreateEnvironmentRequest struct {
	Name          string `json:"name" validate:"required,oneof=dev staging prod"`
	KubeNamespace string `json:"kube_namespace" validate:"required,k8sname"`
}

type BuildRequest struct {
	GitSHA string `json:"git_sha" validate:"required,len=40,hexadecimal"`
}

// Validation functions
func (v *Validator) ValidateStruct(s interface{}) ValidationErrors {
	var errors ValidationErrors

	err := v.validate.Struct(s)
	if err != nil {
		validatorErrors := err.(validator.ValidationErrors)
		for _, validatorError := range validatorErrors {
			errors = append(errors, ValidationError{
				Field:   validatorError.Field(),
				Tag:     validatorError.Tag(),
				Value:   fmt.Sprintf("%v", validatorError.Value()),
				Message: getErrorMessage(validatorError),
			})
		}
	}

	return errors
}

func getErrorMessage(err validator.FieldError) string {
	switch err.Tag() {
	case "required":
		return "This field is required"
	case "min":
		return fmt.Sprintf("Must be at least %s characters long", err.Param())
	case "max":
		return fmt.Sprintf("Must be at most %s characters long", err.Param())
	case "dnsname":
		return "Must be a valid DNS name (lowercase letters, numbers, and hyphens only)"
	case "envvar":
		return "Must be a valid environment variable name (uppercase letters, numbers, and underscores only)"
	case "gitrepo":
		return "Must be a valid Git repository URL"
	case "k8sname":
		return "Must be a valid Kubernetes name"
	case "project_slug":
		return "Must be a valid project slug (3-63 characters, lowercase letters, numbers, and hyphens)"
	case "service_name":
		return "Must be a valid service name (1-63 characters, lowercase letters, numbers, and hyphens)"
	case "safe_string":
		return "Contains invalid characters"
	case "port_number":
		return "Must be a valid port number (1-65535)"
	case "uuid":
		return "Must be a valid UUID"
	case "hexadecimal":
		return "Must be a valid hexadecimal string"
	case "len":
		return fmt.Sprintf("Must be exactly %s characters long", err.Param())
	case "oneof":
		return fmt.Sprintf("Must be one of: %s", err.Param())
	default:
		return "Invalid value"
	}
}

// Custom validation functions
func validateDNSName(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	return dnsNameRegex.MatchString(value)
}

func validateEnvVarName(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	return envVarRegex.MatchString(value)
}

func validateGitRepo(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if !gitRepoRegex.MatchString(value) {
		return false
	}

	// Additional validation: check if URL is parseable
	_, err := url.Parse(value)
	return err == nil
}

func validateK8sName(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	return k8sNameRegex.MatchString(value)
}

func validateProjectSlug(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	return dnsNameRegex.MatchString(value)
}

func validateServiceName(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	if len(value) < 1 || len(value) > 63 {
		return false
	}
	return dnsNameRegex.MatchString(value)
}

func validateSafeString(fl validator.FieldLevel) bool {
	value := fl.Field().String()

	// Check for potentially dangerous characters
	for _, r := range value {
		if r < 32 || r == 127 { // Control characters
			return false
		}
		if r == '<' || r == '>' || r == '"' || r == '\'' || r == '&' {
			return false
		}
	}

	return true
}

func validatePortNumber(fl validator.FieldLevel) bool {
	port := fl.Field().Int()
	return port >= 1 && port <= 65535
}

// Sanitization functions
func SanitizeString(input string) string {
	// Remove control characters and trim whitespace
	var cleaned strings.Builder
	for _, r := range strings.TrimSpace(input) {
		if unicode.IsPrint(r) && r != '<' && r != '>' && r != '"' && r != '\'' && r != '&' {
			cleaned.WriteRune(r)
		}
	}
	return cleaned.String()
}

func SanitizeDNSName(input string) string {
	// Convert to lowercase and remove invalid characters
	input = strings.ToLower(input)
	var cleaned strings.Builder

	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			cleaned.WriteRune(r)
		}
	}

	result := cleaned.String()

	// Remove leading/trailing hyphens
	result = strings.Trim(result, "-")

	// Ensure it's not empty and not too long
	if len(result) == 0 {
		result = "default"
	}
	if len(result) > 63 {
		result = result[:63]
		result = strings.Trim(result, "-")
	}

	return result
}

func SanitizeProjectSlug(input string) string {
	sanitized := SanitizeDNSName(input)

	// Ensure minimum length
	if len(sanitized) < 3 {
		sanitized = sanitized + "-proj"
	}

	return sanitized
}

// Middleware for request validation
func (v *Validator) ValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Store validator in context for use by handlers
		c.Set("validator", v)
		c.Next()
	}
}

// Helper to get validator from context
func GetValidatorFromContext(c *gin.Context) *Validator {
	if validator, exists := c.Get("validator"); exists {
		return validator.(*Validator)
	}
	return NewValidator()
}

// Request binding with validation
func BindAndValidate[T any](c *gin.Context, obj *T) error {
	// Bind JSON
	if err := c.ShouldBindJSON(obj); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate
	validator := GetValidatorFromContext(c)
	if validationErrors := validator.ValidateStruct(obj); len(validationErrors) > 0 {
		return validationErrors
	}

	return nil
}

// UUID validation helper
func ValidateUUID(uuidStr string) (uuid.UUID, error) {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid UUID format: %w", err)
	}
	return id, nil
}

// Path parameter validation
func ValidatePathParam(c *gin.Context, param string, validator func(string) bool, errorMsg string) (string, error) {
	value := c.Param(param)
	if value == "" {
		return "", fmt.Errorf("%s parameter is required", param)
	}

	if !validator(value) {
		return "", fmt.Errorf("%s", errorMsg)
	}

	return value, nil
}

// Query parameter validation
func ValidateQueryParam(c *gin.Context, param string, defaultValue string, validator func(string) bool, errorMsg string) (string, error) {
	value := c.DefaultQuery(param, defaultValue)

	if !validator(value) {
		return "", fmt.Errorf("%s", errorMsg)
	}

	return value, nil
}
