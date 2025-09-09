def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n - 1) + fibonacci(n - 2)

if __name__ == "__main__":
    n = 10
    result = fibonacci(n)
    print(f"The {n}th Fibonacci number is: {result}")