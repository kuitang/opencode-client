#!/usr/bin/env python3
"""
Test script to verify all requested packages are installed and working
"""

import sys
import subprocess

def test_imports():
    """Test importing all Python libraries"""
    print("Testing Python library imports...")

    packages = [
        'numpy',
        'pandas',
        'scipy',
        'matplotlib',
        'sklearn',
        'seaborn',
        'xgboost',
        'yfinance',
        'flask',
        'fastapi',
        'sqlite3'  # Built into Python
    ]

    for package in packages:
        try:
            __import__(package)
            print(f"✓ {package}")
        except ImportError as e:
            print(f"✗ {package}: {e}")
            return False

    return True

def test_basic_functionality():
    """Test basic functionality of key packages"""
    print("\nTesting basic functionality...")

    try:
        import numpy as np
        arr = np.array([1, 2, 3])
        print(f"✓ numpy array: {arr}")

        import pandas as pd
        df = pd.DataFrame({'a': [1, 2], 'b': [3, 4]})
        print(f"✓ pandas DataFrame shape: {df.shape}")

        import sqlite3
        conn = sqlite3.connect(':memory:')
        conn.execute('CREATE TABLE test (id INTEGER)')
        print("✓ sqlite3 in-memory database created")
        conn.close()

        import yfinance as yf
        print("✓ yfinance imported successfully")

    except Exception as e:
        print(f"✗ Functionality test failed: {e}")
        return False

    return True

def test_command_line_tools():
    """Test command line tools are available"""
    print("\nTesting command line tools...")

    tools = [
        ('vi', ['vi', '--version']),
        ('tmux', ['tmux', '-V']),
        ('nano', ['nano', '--version']),
        ('sqlite3', ['sqlite3', '--version'])
    ]

    for tool_name, cmd in tools:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=5)
            if result.returncode == 0:
                version = result.stdout.strip().split('\n')[0]
                print(f"✓ {tool_name}: {version}")
            else:
                print(f"✗ {tool_name}: command failed")
                return False
        except Exception as e:
            print(f"✗ {tool_name}: {e}")
            return False

    return True

def main():
    print("=== Testing All Requested Packages ===")

    success = True
    success &= test_imports()
    success &= test_basic_functionality()
    success &= test_command_line_tools()

    print("\n" + "="*40)
    if success:
        print("🎉 All tests passed!")
        sys.exit(0)
    else:
        print("❌ Some tests failed!")
        sys.exit(1)

if __name__ == "__main__":
    main()